/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// HyperbytedbClusterSpec defines the desired state of HyperbytedbCluster.
type HyperbytedbClusterSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Container image reference. When set with a tag (contains ':'), used as-is.
	// When set without a tag, treated as the repository and combined with Version.
	// When empty, defaults to hyperbytedb:{Version} or hyperbytedb:latest.
	// +optional
	Image string `json:"image,omitempty"`

	// Application version. Drives the container image tag for hyperbytedb and
	// hyperbytedb-proxy (e.g. version "0.8.3" → hyperbytedb:0.8.3). Changing
	// this field triggers a rolling upgrade.
	// +optional
	Version string `json:"version,omitempty"`

	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// When true the operator skips reconciliation entirely, allowing
	// manual maintenance on the cluster.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// +optional
	Server ServerSpec `json:"server,omitempty"`

	// +optional
	Storage StorageSpec `json:"storage,omitempty"`

	// +optional
	Flush FlushSpec `json:"flush,omitempty"`

	// +optional
	ChDB ChDBSpec `json:"chdb,omitempty"`

	// +optional
	Auth AuthSpec `json:"auth,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	Cluster ClusterTuningSpec `json:"cluster,omitempty"`

	// Experimental series sharding. When set, the operator writes a `[sharding]`
	// block into config.toml. A 1-replica CR with sharding.enabled is a
	// 1-member cluster ([cluster] enabled=true). Defaults stay off.
	// +optional
	Sharding *ShardingSpec `json:"sharding,omitempty"`

	// +optional
	Cardinality CardinalitySpec `json:"cardinality,omitempty"`

	// +optional
	StatementSummary StatementSummarySpec `json:"statementSummary,omitempty"`

	// +optional
	HintedHandoff HintedHandoffSpec `json:"hintedHandoff,omitempty"`

	// +optional
	RateLimit RateLimitSpec `json:"rateLimit,omitempty"`

	// +optional
	Retention RetentionSpec `json:"retention,omitempty"`

	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`

	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`

	// +optional
	Failover *FailoverSpec `json:"failover,omitempty"`

	// Proxy deploys a stateless `hyperbytedb-proxy` Deployment in front of the
	// StatefulSet to absorb rolling restarts and drain events without
	// returning errors to clients.
	// +optional
	Proxy *ProxySpec `json:"proxy,omitempty"`

	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +optional
	AdditionalVolumes []corev1.Volume `json:"additionalVolumes,omitempty"`

	// +optional
	AdditionalVolumeMounts []corev1.VolumeMount `json:"additionalVolumeMounts,omitempty"`
}

type ServerSpec struct {
	// +kubebuilder:default=8086
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// +kubebuilder:default=26214400
	MaxBodySizeBytes int64 `json:"maxBodySizeBytes,omitempty"`

	// +kubebuilder:default=30
	RequestTimeoutSecs int32 `json:"requestTimeoutSecs,omitempty"`

	// +kubebuilder:default=30
	QueryTimeoutSecs int32 `json:"queryTimeoutSecs,omitempty"`

	// Maximum concurrent /query requests. 0 = unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxConcurrentQueries int32 `json:"maxConcurrentQueries,omitempty"`

	// +optional
	TLS *TLSSpec `json:"tls,omitempty"`
}

type TLSSpec struct {
	Enabled bool `json:"enabled"`

	// Name of a Secret of type kubernetes.io/tls. When empty the operator
	// generates a self-signed CA and per-node certs.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// +optional
	CertManagerIssuerRef *CertManagerIssuerRef `json:"certManagerIssuerRef,omitempty"`
}

// CertManagerIssuerRef references a cert-manager Issuer or ClusterIssuer.
type CertManagerIssuerRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// +optional
	Group string `json:"group,omitempty"`
}

type StorageSpec struct {
	// +optional
	VolumeClaimTemplate *PersistentVolumeClaimSpec `json:"volumeClaimTemplate,omitempty"`

	// WAL encoding format: "bincode" (default) or "arrow_ipc".
	// +optional
	// +kubebuilder:validation:Enum=bincode;arrow_ipc
	// +kubebuilder:default="bincode"
	WALFormat string `json:"walFormat,omitempty"`
}

type PersistentVolumeClaimSpec struct {
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// +kubebuilder:default="10Gi"
	Size resource.Quantity `json:"size,omitempty"`
}

type FlushSpec struct {
	// +kubebuilder:default=10
	IntervalSecs int32 `json:"intervalSecs,omitempty"`

	// Max points per chDB insert batch (clamped server-side to 10k–500k).
	// +kubebuilder:default=50000
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxPointsPerBatch int32 `json:"maxPointsPerBatch,omitempty"`

	// WAL group-commit: max entries to coalesce per write batch. 0 = disabled.
	// +optional
	// +kubebuilder:validation:Minimum=0
	WALBatchSize int32 `json:"walBatchSize,omitempty"`

	// WAL group-commit: max microseconds to wait for more entries before flushing.
	// +optional
	// +kubebuilder:validation:Minimum=0
	WALBatchDelayUs int64 `json:"walBatchDelayUs,omitempty"`

	// Keep chDB-ready Arrow batches in an in-memory WAL cache for zero-copy flush.
	// +optional
	// +kubebuilder:default=true
	ArrowWALEnabled *bool `json:"arrowWALEnabled,omitempty"`
}

type ChDBSpec struct {
	// chDB session directory inside the data volume. Defaults to
	// /var/lib/hyperbytedb/chdb when unset.
	// +optional
	SessionDataPath string `json:"sessionDataPath,omitempty"`

	// Sets both query and write pool sizes when QueryPoolSize and WritePoolSize
	// are unset. Defaults to 1 when all pool fields are unset.
	// +optional
	// +kubebuilder:validation:Minimum=1
	PoolSize int32 `json:"poolSize,omitempty"`

	// chDB connections reserved for queries. Isolated from ingest/flush.
	// +optional
	// +kubebuilder:validation:Minimum=1
	QueryPoolSize int32 `json:"queryPoolSize,omitempty"`

	// chDB connections reserved for ingest and flush (Arrow WAL build, INSERTs).
	// +optional
	// +kubebuilder:validation:Minimum=1
	WritePoolSize int32 `json:"writePoolSize,omitempty"`
}

type AuthSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Reference to a Secret containing auth credentials.
	// +optional
	CredentialsSecretName string `json:"credentialsSecretName,omitempty"`
}

type LoggingSpec struct {
	// +kubebuilder:default="info"
	// +kubebuilder:validation:Enum=trace;debug;info;warn;error
	Level string `json:"level,omitempty"`

	// +kubebuilder:default="text"
	// +kubebuilder:validation:Enum=text;json
	Format string `json:"format,omitempty"`
}

type ClusterTuningSpec struct {
	// +kubebuilder:default=2
	HeartbeatIntervalSecs int32 `json:"heartbeatIntervalSecs,omitempty"`

	// +kubebuilder:default=5
	HeartbeatMissThreshold int32 `json:"heartbeatMissThreshold,omitempty"`

	// +kubebuilder:default=5
	ReplicationMaxRetries int32 `json:"replicationMaxRetries,omitempty"`

	// +kubebuilder:default=1000
	RaftHeartbeatIntervalMs int32 `json:"raftHeartbeatIntervalMs,omitempty"`

	// +kubebuilder:default=1000
	RaftElectionTimeoutMs int32 `json:"raftElectionTimeoutMs,omitempty"`

	// +kubebuilder:default=1000
	RaftSnapshotThreshold int32 `json:"raftSnapshotThreshold,omitempty"`

	// Bounded outbound replication queue depth (ingest-sized batches).
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReplicationQueueDepth int32 `json:"replicationQueueDepth,omitempty"`

	// Max concurrent outbound replication fan-out rounds (token bucket).
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReplicationMaxInflightBatches int32 `json:"replicationMaxInflightBatches,omitempty"`

	// Max bytes for coalescing consecutive WAL batches with the same db/rp/precision.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReplicationMaxCoalesceBodyBytes int64 `json:"replicationMaxCoalesceBodyBytes,omitempty"`

	// Bounded apply queue on the replicate receiver.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReplicateReceiverQueueDepth int32 `json:"replicateReceiverQueueDepth,omitempty"`

	// When >0, peers with ack 0 and stale heartbeats (older than
	// heartbeatIntervalSecs * multiplier) are omitted from the WAL truncate barrier.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ReplicationTruncateStalePeerMultiplier int64 `json:"replicationTruncateStalePeerMultiplier,omitempty"`

	// Per-node, per-write replication mode and tuning.
	// +optional
	Replication *ReplicationSpec `json:"replication,omitempty"`

	// TLS for inter-node replication traffic.
	// +optional
	TLS *TLSSpec `json:"tls,omitempty"`

	// Seconds to wait after excluding a backend from the proxy before
	// deleting its pod. Gives in-flight requests time to drain.
	// +kubebuilder:default=10
	// +optional
	DrainWaitSecs int32 `json:"drainWaitSecs,omitempty"`
}

// ShardingSpec maps to HyperbyteDB `[sharding]` (experimental series_id range
// sharding). Omitted fields are left out of config.toml so the server defaults apply.
// When enabled, config validation requires regionMergeSeries < regionSplitSeries
// < regionMaxSeries (same inequalities as hyperbytedb).
type ShardingSpec struct {
	// Master switch. Written as sharding.enabled.
	Enabled bool `json:"enabled"`

	// Target replica count per shard region.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ReplicationFactor int32 `json:"replicationFactor,omitempty"`

	// Target series per region before split.
	// +optional
	// +kubebuilder:validation:Minimum=1
	RegionSplitSeries int64 `json:"regionSplitSeries,omitempty"`

	// Hard split threshold (must be > regionSplitSeries when both are set).
	// +optional
	// +kubebuilder:validation:Minimum=1
	RegionMaxSeries int64 `json:"regionMaxSeries,omitempty"`

	// Merge when adjacent regions fall below this (must be < regionSplitSeries when both are set).
	// +optional
	// +kubebuilder:validation:Minimum=1
	RegionMergeSeries int64 `json:"regionMergeSeries,omitempty"`

	// Cooldown between split/merge on a region.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SplitMergeIntervalSecs int64 `json:"splitMergeIntervalSecs,omitempty"`

	// Max concurrent split/move/merge operators.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ScheduleLimit int32 `json:"scheduleLimit,omitempty"`

	// Region-stats report interval and shard scheduler tick (same duration).
	// +optional
	// +kubebuilder:validation:Minimum=1
	HeartbeatIntervalSecs int64 `json:"heartbeatIntervalSecs,omitempty"`

	// Sync bootstrap RPC timeout.
	// +optional
	// +kubebuilder:validation:Minimum=1
	BootstrapTimeoutMs int64 `json:"bootstrapTimeoutMs,omitempty"`

	// Seconds before the Raft leader proposes TransferPrimary for an unhealthy primary.
	// +optional
	// +kubebuilder:validation:Minimum=1
	PrimaryFailoverAfterSecs int64 `json:"primaryFailoverAfterSecs,omitempty"`

	// Per-peer HTTP timeout for sharded query/write/metadata scatter.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ScatterPeerTimeoutMs int64 `json:"scatterPeerTimeoutMs,omitempty"`

	// Max Active peers tried per region per scatter request.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ScatterMaxPeerAttempts int32 `json:"scatterMaxPeerAttempts,omitempty"`

	// Load-based split QPS threshold; 0 = disabled. Emitted only when set so the
	// server default (0) applies when omitted.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LoadSplitQpsThreshold *int64 `json:"loadSplitQpsThreshold,omitempty"`

	// Hard cap on regions per measurement.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxRegionsPerMeasurement int32 `json:"maxRegionsPerMeasurement,omitempty"`
}

// ReplicationSpec controls coordinator-side replication (how this node's
// accepted client writes are replicated to peers).
type ReplicationSpec struct {
	// Replication mode. "async" is fire-and-forget HTTP fan-out (default,
	// preserves today's behavior). "sync_quorum" awaits W-of-N peer acks
	// before returning to the client.
	// +optional
	// +kubebuilder:default="async"
	// +kubebuilder:validation:Enum=async;sync_quorum
	Mode string `json:"mode,omitempty"`

	// Worst-case latency budget (ms) for sync_quorum writes. On timeout the
	// coordinator returns 504; in-flight peer tasks keep running and unacked
	// peers fall back to hinted handoff.
	// +optional
	// +kubebuilder:default=5000
	// +kubebuilder:validation:Minimum=0
	AckTimeoutMs int64 `json:"ackTimeoutMs,omitempty"`

	// +optional
	SyncQuorum *SyncQuorumSpec `json:"syncQuorum,omitempty"`
}

// SyncQuorumSpec configures the sync_quorum replication mode.
type SyncQuorumSpec struct {
	// Number of peer acks required for sync_quorum. Either the string
	// "majority" (resolved at request time against current active peers) or an
	// explicit integer count. The local WAL append always happens first, so
	// self-durability is implicit and the local node is never counted toward
	// the quorum.
	// +optional
	MinAcks *intstr.IntOrString `json:"minAcks,omitempty"`
}

// CardinalitySpec configures cardinality limits enforced by hyperbytedb.
type CardinalitySpec struct {
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxTagValuesPerMeasurement int64 `json:"maxTagValuesPerMeasurement,omitempty"`

	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxMeasurementsPerDatabase int64 `json:"maxMeasurementsPerDatabase,omitempty"`
}

// StatementSummarySpec controls collection of per-statement execution stats
// exposed via /debug/statement_summary.
type StatementSummarySpec struct {
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Maximum number of distinct statements tracked. Oldest entries are evicted
	// when the limit is exceeded.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxEntries int32 `json:"maxEntries,omitempty"`
}

// HintedHandoffSpec configures the hinted-handoff queue used to retry writes
// against peers that were temporarily unreachable.
type HintedHandoffSpec struct {
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Maximum queued hints per unreachable peer before oldest are dropped.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxHintsPerPeer int64 `json:"maxHintsPerPeer,omitempty"`

	// Hints older than this (seconds) are discarded on drain.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxHintAgeSecs int64 `json:"maxHintAgeSecs,omitempty"`
}

// RetentionSpec controls the background retention enforcement loop.
type RetentionSpec struct {
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// How often retention scans run (humantime duration, e.g. "60s", "5m", "1h").
	// +kubebuilder:default="60s"
	// +optional
	Interval string `json:"interval,omitempty"`
}

// RateLimitSpec controls per-endpoint request rate limiting.
type RateLimitSpec struct {
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Maximum requests per second per endpoint (/write, /query). 0 = unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxRequestsPerSecond int64 `json:"maxRequestsPerSecond,omitempty"`
}

type MonitoringSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:default=true
	ServiceMonitor bool `json:"serviceMonitor,omitempty"`
}

type AutoscalingSpec struct {
	Enabled bool `json:"enabled"`

	// +kubebuilder:validation:Minimum=1
	MinReplicas int32 `json:"minReplicas,omitempty"`

	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// +kubebuilder:default=80
	TargetCPUUtilizationPercentage int32 `json:"targetCPUUtilizationPercentage,omitempty"`
}

// FailoverSpec controls automatic failure detection and recovery.
type FailoverSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Maximum number of simultaneous failovers.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	MaxFailoverCount int32 `json:"maxFailoverCount,omitempty"`

	// Seconds a member must be unhealthy before triggering failover.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=60
	FailoverTimeoutSecs int32 `json:"failoverTimeoutSecs,omitempty"`
}

// ProxySpec configures the optional `hyperbytedb-proxy` reverse proxy that
// sits in front of the StatefulSet. The proxy is health-aware: it routes
// only to Active backends and holds requests briefly while a rolling
// restart cycles through pods, so clients (Grafana, Telegraf, etc.) never
// observe transient 503s.
type ProxySpec struct {
	// When false, the operator does not create or reconcile any proxy
	// resources. Existing proxy Deployment/Service (if any) are left alone
	// so they can be cleaned up out-of-band.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// +kubebuilder:default="ghcr.io/hyperbyte-cloud/hyperbytedb-proxy"
	Image string `json:"image,omitempty"`

	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=1
	Replicas *int32 `json:"replicas,omitempty"`

	// Port the proxy Service exposes. Defaults to the cluster server port so
	// existing clients can re-target the Service name with no port change.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// HTTP path used for backend health probes. Defaults to `/health`.
	// Set to `/health/ready` for the deeper chDB-aware readiness check.
	// +optional
	HealthPath string `json:"healthPath,omitempty"`

	// How long the proxy waits for a backend to come back before failing a
	// request with 503. Bigger values mean rolling restarts are smoother but
	// individual stuck requests sit longer.
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=0
	HoldTimeoutSecs int32 `json:"holdTimeoutSecs,omitempty"`

	// Cap on per-backend retries for one request. 0 disables retries.
	// +optional
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=0
	MaxRetries int32 `json:"maxRetries,omitempty"`

	// How long the proxy keeps serving in-flight requests after SIGTERM
	// before exiting. Should comfortably exceed the longest expected query.
	// +optional
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=1
	ShutdownGraceSecs int32 `json:"shutdownGraceSecs,omitempty"`

	// Per-request budget the proxy allows for the upstream call. Defaults
	// to ~ServerSpec.RequestTimeoutSecs.
	// +optional
	// +kubebuilder:validation:Minimum=1
	RequestTimeoutSecs int32 `json:"requestTimeoutSecs,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Type of the proxy Service. Defaults to ClusterIP. Set NodePort/
	// LoadBalancer to expose externally.
	// +optional
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// Explicit nodePort when ServiceType=NodePort. Required for kind clusters
	// that pre-map a host port to a fixed nodePort.
	// +optional
	// +kubebuilder:validation:Minimum=30000
	// +kubebuilder:validation:Maximum=32767
	NodePort int32 `json:"nodePort,omitempty"`

	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
}

// ClusterPhase represents the lifecycle phase of the cluster.
// +kubebuilder:validation:Enum=Pending;Initializing;Running;Scaling;Upgrading;Failed
type ClusterPhase string

const (
	ClusterPhasePending      ClusterPhase = "Pending"
	ClusterPhaseInitializing ClusterPhase = "Initializing"
	ClusterPhaseRunning      ClusterPhase = "Running"
	ClusterPhaseScaling      ClusterPhase = "Scaling"
	ClusterPhaseUpgrading    ClusterPhase = "Upgrading"
	ClusterPhaseFailed       ClusterPhase = "Failed"
)

// RollingRestartPhase tracks which step of the proxy-coordinated rolling
// restart the operator is currently executing.
type RollingRestartPhase string

const (
	RollingRestartExcluding    RollingRestartPhase = "Excluding"
	RollingRestartDraining     RollingRestartPhase = "Draining"
	RollingRestartWaitingReady RollingRestartPhase = "WaitingReady"
	RollingRestartIncluding    RollingRestartPhase = "Including"
	RollingRestartCompleted    RollingRestartPhase = "Completed"
)

// RollingRestartState tracks pod-by-pod proxy exclusion during rolling upgrades.
// Nil when no rolling restart is in progress.
type RollingRestartState struct {
	// Ordinal of the pod currently being restarted.
	CurrentOrdinal int32 `json:"currentOrdinal"`

	// Total number of pod ordinals to cycle through.
	TotalOrdinals int32 `json:"totalOrdinals"`

	// Current phase of the restart state machine.
	Phase RollingRestartPhase `json:"phase"`

	// When the current phase started (used to compute drain wait).
	PhaseStartedAt metav1.Time `json:"phaseStartedAt"`

	// IP of the pod being excluded (set during Excluding phase).
	OldPodIP string `json:"oldPodIP,omitempty"`

	// True once the proxy confirms the backend is excluded.
	ExcludeConfirmed bool `json:"excludeConfirmed"`
}

// MemberStatus describes the observed state of a single cluster member.
type MemberStatus struct {
	// Stable identifier derived from the StatefulSet ordinal.
	Name string `json:"name"`

	// Hyperbytedb node_id (ordinal + 1).
	NodeID int32 `json:"nodeId"`

	// Kubernetes Pod name.
	PodName string `json:"podName"`

	// State reported by the node's /health endpoint (Active, Syncing, Joining, etc.).
	State string `json:"state"`

	// True when the pod passes its readiness probe.
	Health bool `json:"health"`

	// Last WAL sequence number on this node.
	// +optional
	WALSequence int64 `json:"walSequence,omitempty"`

	// Total number of parquet files on this node.
	// +optional
	ParquetFiles int32 `json:"parquetFiles,omitempty"`

	// Number of peers this node is aware of.
	// +optional
	PeerCount int32 `json:"peerCount,omitempty"`

	// Last time the member transitioned state.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
}

// HyperbytedbClusterStatus defines the observed state of HyperbytedbCluster.
type HyperbytedbClusterStatus struct {
	// +kubebuilder:default="Pending"
	Phase ClusterPhase `json:"phase,omitempty"`

	Replicas      int32 `json:"replicas,omitempty"`
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// High-level cluster health: Healthy, Degraded, Recovering, Unknown.
	// +optional
	ClusterState string `json:"clusterState,omitempty"`

	// Replication convergence state: Healthy, Lagging, Diverged, Unknown.
	// +optional
	ReplicationState string `json:"replicationState,omitempty"`

	// Per-member status.
	// +optional
	Members []MemberStatus `json:"members,omitempty"`

	// Number of failovers executed in the current generation.
	// +optional
	FailoverCount int32 `json:"failoverCount,omitempty"`

	// Hash of the current config.toml used for rolling update detection.
	// +optional
	ConfigHash string `json:"configHash,omitempty"`

	// Tracks pod-by-pod proxy exclusion during rolling upgrades.
	// Nil when no rolling restart is in progress.
	// +optional
	RollingRestart *RollingRestartState `json:"rollingRestart,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.clusterState`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// HyperbytedbCluster is the Schema for the hyperbytedbclusters API.
type HyperbytedbCluster struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec HyperbytedbClusterSpec `json:"spec"`

	// +optional
	Status HyperbytedbClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// HyperbytedbClusterList contains a list of HyperbytedbCluster.
type HyperbytedbClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HyperbytedbCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HyperbytedbCluster{}, &HyperbytedbClusterList{})
}
