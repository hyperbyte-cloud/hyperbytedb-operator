package hyperbytedb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with hyperbytedb's HTTP cluster management API.
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type ClusterMetrics struct {
	Mode      string   `json:"mode"`
	NodeAddr  string   `json:"node_addr"`
	NodeID    int      `json:"node_id"`
	Peers     []string `json:"peers"`
	PeerCount int      `json:"peer_count"`
}

// NodeInfo represents a single cluster member returned by /cluster/nodes.
type NodeInfo struct {
	NodeID        int    `json:"node_id"`
	Addr          string `json:"addr"`
	State         string `json:"state"`
	Self          bool   `json:"self"`
	LastHeartbeat int64  `json:"last_heartbeat"`
}

// HealthStatus represents the structured response from /health.
type HealthStatus struct {
	Status  string `json:"status"` // "pass" (healthy), "syncing", "joining", "draining", "leaving"
	Message string `json:"message,omitempty"`
	NodeID  int    `json:"node_id,omitempty"`
}

// clusterNodesResponse wraps the /cluster/nodes JSON envelope.
type clusterNodesResponse struct {
	Nodes []NodeInfo `json:"nodes"`
}

func (c *Client) GetClusterMetrics(ctx context.Context, host string, port int32) (*ClusterMetrics, error) {
	url := fmt.Sprintf("http://%s:%d/cluster/metrics", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cluster/metrics returned %d: %s", resp.StatusCode, string(respBody))
	}
	var metrics ClusterMetrics
	if err := json.NewDecoder(resp.Body).Decode(&metrics); err != nil {
		return nil, fmt.Errorf("decoding cluster metrics: %w", err)
	}
	return &metrics, nil
}

// GetClusterNodes returns the list of known cluster members from a node.
func (c *Client) GetClusterNodes(ctx context.Context, host string, port int32) ([]NodeInfo, error) {
	url := fmt.Sprintf("http://%s:%d/cluster/nodes", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cluster/nodes returned %d: %s", resp.StatusCode, string(respBody))
	}
	var envelope clusterNodesResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding cluster nodes: %w", err)
	}
	return envelope.Nodes, nil
}

// DrainNode tells a node to stop accepting new writes and finish in-flight
// replication before it shuts down.
func (c *Client) DrainNode(ctx context.Context, host string, port int32) error {
	url := fmt.Sprintf("http://%s:%d/internal/drain", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusNoContent &&
		resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("drain returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// GetNodeHealth returns structured health information including node state.
func (c *Client) GetNodeHealth(ctx context.Context, host string, port int32) (*HealthStatus, error) {
	url := fmt.Sprintf("http://%s:%d/health", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading health response: %w", err)
	}

	hs := &HealthStatus{}
	if err := json.Unmarshal(body, hs); err != nil {
		// Fall back: non-JSON response means basic health endpoint
		if resp.StatusCode == http.StatusOK {
			hs.Status = "Active"
		} else {
			hs.Status = "Unhealthy"
		}
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		if hs.Status == "" {
			hs.Status = "Unavailable"
		}
	}
	return hs, nil
}

// CheckHealth performs a simple liveness check against /health.
func (c *Client) CheckHealth(ctx context.Context, host string, port int32) error {
	url := fmt.Sprintf("http://%s:%d/health", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}

// SyncManifest mirrors the server's /internal/sync/manifest response.
type SyncManifest struct {
	NodeID     int                `json:"node_id"`
	WALLastSeq int64              `json:"wal_last_seq"`
	Databases  []DatabaseManifest `json:"databases"`
}

// DatabaseManifest describes a single database in the sync manifest.
type DatabaseManifest struct {
	Name         string                `json:"name"`
	Measurements []MeasurementManifest `json:"measurements"`
}

// MeasurementManifest describes a single measurement in the sync manifest.
type MeasurementManifest struct {
	Name         string                `json:"name"`
	RP           string                `json:"rp"`
	ParquetFiles []ParquetFileManifest `json:"parquet_files"`
}

// ParquetFileManifest describes a single parquet file.
type ParquetFileManifest struct {
	Path         string  `json:"path"`
	SizeBytes    int64   `json:"size_bytes"`
	OriginNodeID *uint64 `json:"origin_node_id,omitempty"`
	MinTime      int64   `json:"min_time"`
	MaxTime      int64   `json:"max_time"`
	Checksum     uint32  `json:"checksum"`
}

// TotalParquetFiles returns the aggregate file count across all databases.
func (m *SyncManifest) TotalParquetFiles() int {
	total := 0
	for _, db := range m.Databases {
		for _, meas := range db.Measurements {
			total += len(meas.ParquetFiles)
		}
	}
	return total
}

// GetSyncManifest retrieves the node's sync manifest containing WAL
// sequence and parquet file inventory.
func (c *Client) GetSyncManifest(ctx context.Context, host string, port int32) (*SyncManifest, error) {
	url := fmt.Sprintf("http://%s:%d/internal/sync/manifest", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sync/manifest returned %d: %s", resp.StatusCode, string(respBody))
	}
	var manifest SyncManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decoding sync manifest: %w", err)
	}
	return &manifest, nil
}

// joinRequest is the body for POST /internal/membership/join.
type joinRequest struct {
	NodeID *int   `json:"node_id,omitempty"`
	Addr   string `json:"addr"`
}

// JoinNode registers a node with the cluster by sending a join request
// to the target host.
func (c *Client) JoinNode(ctx context.Context, host string, port int32, nodeID int, nodeAddr string) error {
	url := fmt.Sprintf("http://%s:%d/internal/membership/join", host, port)
	body, err := json.Marshal(joinRequest{NodeID: &nodeID, Addr: nodeAddr})
	if err != nil {
		return fmt.Errorf("marshalling join request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("membership/join returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// LeaderInfo is the response from GET /cluster/leader.
type LeaderInfo struct {
	LeaderID   *uint64 `json:"leader_id"`
	LeaderAddr *string `json:"leader_addr"`
	ThisNodeID uint64  `json:"this_node_id"`
	IsLeader   bool    `json:"is_leader"`
	Term       uint64  `json:"term"`
}

// GetClusterLeader returns the Raft leader's id/address as seen by `host`.
// Use this to find the leader to direct membership changes at.
func (c *Client) GetClusterLeader(ctx context.Context, host string, port int32) (*LeaderInfo, error) {
	url := fmt.Sprintf("http://%s:%d/cluster/leader", host, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cluster/leader returned %d: %s", resp.StatusCode, string(body))
	}
	var info LeaderInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decoding cluster leader: %w", err)
	}
	return &info, nil
}

// addNodeRequest is the body for POST /cluster/membership/add-node.
type addNodeRequest struct {
	NodeID  uint64 `json:"node_id"`
	Addr    string `json:"addr"`
	Promote bool   `json:"promote"`
}

// AddNodeResponse is the response from POST /cluster/membership/add-node.
type AddNodeResponse struct {
	NodeID          uint64  `json:"node_id"`
	Addr            string  `json:"addr"`
	AddedAsLearner  bool    `json:"added_as_learner"`
	PromotedToVoter bool    `json:"promoted_to_voter"`
	LeaderID        *uint64 `json:"leader_id"`
}

// ErrNotLeader is returned by AddClusterNode when the target host is not the
// Raft leader. The leader's id (if known) is in LeaderID so the caller can
// retry against the right host.
type ErrNotLeader struct {
	LeaderID *uint64
}

func (e *ErrNotLeader) Error() string {
	if e.LeaderID == nil {
		return "target node is not the raft leader (leader unknown)"
	}
	return fmt.Sprintf("target node is not the raft leader (leader_id=%d)", *e.LeaderID)
}

// AddClusterNode asks `host` (which must be the current Raft leader) to add
// the node identified by `nodeID`/`nodeAddr` as a Raft learner and, when
// `promote` is true, promote it to a voter in the same call. Returns
// *ErrNotLeader if the target is not currently the leader.
func (c *Client) AddClusterNode(ctx context.Context, host string, port int32, nodeID uint64, nodeAddr string, promote bool) (*AddNodeResponse, error) {
	url := fmt.Sprintf("http://%s:%d/cluster/membership/add-node", host, port)
	body, err := json.Marshal(addNodeRequest{NodeID: nodeID, Addr: nodeAddr, Promote: promote})
	if err != nil {
		return nil, fmt.Errorf("marshalling add-node request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusServiceUnavailable {
		var errResp struct {
			Error    string  `json:"error"`
			LeaderID *uint64 `json:"leader_id"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return nil, &ErrNotLeader{LeaderID: errResp.LeaderID}
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("membership/add-node returned %d: %s", resp.StatusCode, string(respBody))
	}

	var out AddNodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding add-node response: %w", err)
	}
	return &out, nil
}

// leaveRequest is the body for POST /internal/membership/leave.
type leaveRequest struct {
	NodeID uint64 `json:"node_id"`
}

// LeaveNode notifies a surviving member to remove a departed node from in-memory
// membership (and replication peers). Call after drain on the departing pod.
func (c *Client) LeaveNode(ctx context.Context, host string, port int32, departedNodeID uint64) error {
	url := fmt.Sprintf("http://%s:%d/internal/membership/leave", host, port)
	body, err := json.Marshal(leaveRequest{NodeID: departedNodeID})
	if err != nil {
		return fmt.Errorf("marshalling leave request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("membership/leave returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// ---------- Proxy backend exclusion ----------

// ProxyBackendStatus mirrors the JSON returned by the proxy's GET /admin/pool.
type ProxyBackendStatus struct {
	Addr                string `json:"addr"`
	Port                int    `json:"port"`
	Health              string `json:"health"`
	Excluded            bool   `json:"excluded"`
	Inflight            int    `json:"inflight"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
}

// ExcludeProxyBackend tells the proxy to stop routing to the given backend IP.
func (c *Client) ExcludeProxyBackend(ctx context.Context, proxyHost string, port int32, ip string) error {
	url := fmt.Sprintf("http://%s:%d/admin/backends/%s/exclude", proxyHost, port, ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("exclude backend %s: %w", ip, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("exclude backend %s returned %d: %s", ip, resp.StatusCode, string(respBody))
	}
	return nil
}

// IncludeProxyBackend tells the proxy to resume routing to the given backend IP.
func (c *Client) IncludeProxyBackend(ctx context.Context, proxyHost string, port int32, ip string) error {
	url := fmt.Sprintf("http://%s:%d/admin/backends/%s/include", proxyHost, port, ip)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("include backend %s: %w", ip, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("include backend %s returned %d: %s", ip, resp.StatusCode, string(respBody))
	}
	return nil
}

// GetProxyPoolState retrieves the full pool status from the proxy's admin API.
func (c *Client) GetProxyPoolState(ctx context.Context, proxyHost string, port int32) ([]ProxyBackendStatus, error) {
	url := fmt.Sprintf("http://%s:%d/admin/pool", proxyHost, port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get proxy pool state: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("proxy pool state returned %d: %s", resp.StatusCode, string(respBody))
	}
	var pool []ProxyBackendStatus
	if err := json.NewDecoder(resp.Body).Decode(&pool); err != nil {
		return nil, fmt.Errorf("decoding proxy pool state: %w", err)
	}
	return pool, nil
}
