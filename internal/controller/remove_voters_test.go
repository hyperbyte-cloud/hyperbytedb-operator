package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"

	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

// fakeRaft is a shared view of one Raft group served by several fake nodes.
type fakeRaft struct {
	mu      sync.Mutex
	leader  uint64
	voters  map[uint64]bool
	removed []uint64
}

// handler serves the leader and remove-node endpoints as node `id` sees them.
func (f *fakeRaft) handler(id uint64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/cluster/leader":
			fmt.Fprintf(w, `{"leader_id":%d,"this_node_id":%d,"is_leader":%t,"term":1}`,
				f.leader, id, f.leader == id)
		case "/cluster/membership/remove-node":
			if f.leader != id {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"error":"not raft leader","leader_id":%d}`, f.leader)
				return
			}
			var req struct {
				NodeID uint64 `json:"node_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			was := f.voters[req.NodeID]
			delete(f.voters, req.NodeID)
			if was {
				f.removed = append(f.removed, req.NodeID)
			}
			// A leader that removes itself steps down; the lowest survivor
			// takes over.
			if req.NodeID == f.leader {
				f.leader = 0
				for v := range f.voters {
					if f.leader == 0 || v < f.leader {
						f.leader = v
					}
				}
			}
			fmt.Fprintf(w, `{"node_id":%d,"removed_from_voters":%t,"leader_id":%d,"remaining_voters":[]}`,
				req.NodeID, was, id)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestRemoveDepartedVotersFollowsTheLeader(t *testing.T) {
	// Node 6 leads and is itself departing, the case that forces a second
	// leader lookup. 4..6 must all leave the voter set, highest first.
	f := &fakeRaft{leader: 6, voters: map[uint64]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true}}

	// The client uses one port for every node, so give each node its own
	// loopback address on a shared port.
	hosts := map[uint64]string{}
	var port int32
	for id := uint64(1); id <= 6; id++ {
		srv := httptest.NewUnstartedServer(f.handler(id))
		addr := fmt.Sprintf("127.0.0.%d:0", id)
		if port != 0 {
			addr = fmt.Sprintf("127.0.0.%d:%d", id, port)
		}
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Skipf("cannot bind %s: %v", addr, err)
		}
		srv.Listener = l
		srv.Start()
		t.Cleanup(srv.Close)
		u, _ := url.Parse(srv.URL)
		p, _ := strconv.Atoi(u.Port())
		port = int32(p)
		hosts[id] = u.Hostname()
	}

	err := removeDepartedVoters(context.Background(), hyperbytedb.NewClient(), hosts, port, []uint64{6, 5, 4})
	if err != nil {
		t.Fatalf("removeDepartedVoters: %v", err)
	}
	if got := fmt.Sprint(f.removed); got != "[6 5 4]" {
		t.Errorf("removed = %s, want [6 5 4]", got)
	}
	for id := uint64(1); id <= 3; id++ {
		if !f.voters[id] {
			t.Errorf("survivor %d was removed", id)
		}
	}

	// Retrying after success is a no-op, not an error: the operator re-runs
	// the hook every reconcile until the StatefulSet has shrunk.
	if err := removeDepartedVoters(context.Background(), hyperbytedb.NewClient(), hosts, port, []uint64{6, 5, 4}); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestRemoveDepartedVotersFailsWithoutALeader(t *testing.T) {
	// No leader visible must hold the scale-down, not skip the removal.
	hosts := map[uint64]string{1: "127.0.0.1"}
	err := removeDepartedVoters(context.Background(), hyperbytedb.NewClient(), hosts, 1, []uint64{2})
	if err == nil {
		t.Fatal("expected an error when no leader is reachable")
	}
}
