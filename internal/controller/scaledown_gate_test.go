package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

// healthServer serves /health reporting a fixed node state, and records
// whether /internal/decommission was called.
func healthServer(t *testing.T, state string, calls *int) (host string, port int32) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/internal/decommission" {
			*calls++
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"status":"warn","message":"node is %s","state":%q}`, state, state)
	}))
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse test server url: %v", err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}
	return u.Hostname(), int32(p)
}

func TestHealthStateIsParsedFromTheBody(t *testing.T) {
	// The operator gates scale-down on `state`, not `status`: a node that is
	// draining and one that has fully evacuated both report status "warn".
	for _, want := range []string{"decommissioning", "leaving", "active"} {
		host, port := healthServer(t, want, new(int))
		got, err := hyperbytedb.NewClient().GetNodeHealth(context.Background(), host, port)
		if err != nil {
			t.Fatalf("GetNodeHealth(%s): %v", want, err)
		}
		if got.State != want {
			t.Errorf("state = %q, want %q", got.State, want)
		}
	}
}

func TestDecommissionNodeHitsTheEndpoint(t *testing.T) {
	calls := 0
	host, port := healthServer(t, "active", &calls)
	if err := hyperbytedb.NewClient().DecommissionNode(context.Background(), host, port); err != nil {
		t.Fatalf("DecommissionNode: %v", err)
	}
	if calls != 1 {
		t.Errorf("decommission endpoint called %d times, want 1", calls)
	}
}

func TestDepartingNodeAction(t *testing.T) {
	// The gate exists because evacuation time is a function of how much data a
	// node holds, so no fixed grace period is safe. The node itself says when
	// it is done, by reaching "leaving".
	tests := []struct {
		name             string
		state            string
		reachable        bool
		wantDecommission bool
		wantGate         bool
	}{
		{"still serving: tell it to go, and wait", "active", true, true, true},
		{"already told: keep waiting", nodeStateDecommissioning, true, false, true},
		{"evacuated: safe to delete pod and volume", nodeStateLeaving, true, false, false},
		{"draining is a restart, not a departure", "draining", true, true, true},
		{"unreachable: never gate, or scale-down wedges forever", "", false, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decommission, gate := departingNodeAction(tc.state, tc.reachable)
			if decommission != tc.wantDecommission {
				t.Errorf("decommission = %v, want %v", decommission, tc.wantDecommission)
			}
			if gate != tc.wantGate {
				t.Errorf("gate = %v, want %v", gate, tc.wantGate)
			}
		})
	}
}
