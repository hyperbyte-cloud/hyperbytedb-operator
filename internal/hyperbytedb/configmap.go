package hyperbytedb

import (
	"crypto/sha256"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

const (
	defaultChdbSessionPath = "/var/lib/hyperbytedb/chdb"
	defaultWalDir          = "/var/lib/hyperbytedb/wal"
	defaultMetaDir         = "/var/lib/hyperbytedb/meta"
	defaultRaftDir         = "/var/lib/hyperbytedb/raft"
	defaultReplLogDir      = "/var/lib/hyperbytedb/replication_log"
)

func ConfigMapName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name + "-config"
}

func BuildConfigMap(cluster *v1alpha1.HyperbytedbCluster) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ConfigMapName(cluster),
			Namespace: cluster.Namespace,
			Labels:    CommonLabels(cluster),
		},
		Data: map[string]string{
			"config.toml": renderConfigTOML(cluster),
		},
	}
}

// ConfigHash returns a short SHA-256 digest used only as a StatefulSet pod-template annotation
// to trigger rolling restarts when operator-managed **static** settings change.
//
// Replica count must not affect this hash: scaling up/down updates the mounted ConfigMap
// and Raft membership (via the /cluster/membership/add-node API) without recycling existing
// pods. The live config.toml still sets [cluster].enabled from replica count; only this
// digest ignores that toggle.
func ConfigHash(cluster *v1alpha1.HyperbytedbCluster) string {
	h := sha256.Sum256([]byte(renderConfigTOMLWithClusterEnabled(cluster, false)))
	return fmt.Sprintf("%x", h[:8])
}

func renderConfigTOML(cluster *v1alpha1.HyperbytedbCluster) string {
	return renderConfigTOMLWithClusterEnabled(cluster, clusterMetadataEnabled(cluster))
}

func clusterMetadataEnabled(cluster *v1alpha1.HyperbytedbCluster) bool {
	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}
	return replicas > 1
}

func renderConfigTOMLWithClusterEnabled(cluster *v1alpha1.HyperbytedbCluster, clusterEnabled bool) string {
	spec := &cluster.Spec
	var b strings.Builder

	writeServerSection(&b, spec)
	writeStorageSection(&b, spec)
	writeFlushSection(&b, spec)
	writeChdbSection(&b, spec)
	writeAuthSection(&b, spec)
	writeCardinalitySection(&b, spec)
	writeLoggingSection(&b, spec)
	writeStatementSummarySection(&b, spec)
	writeHintedHandoffSection(&b, spec)
	writeRateLimitSection(&b, spec)
	writeRetentionSection(&b, spec)
	writeClusterSection(&b, cluster, clusterEnabled)

	return b.String()
}

func writeServerSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("[server]\n")
	b.WriteString("bind_address = \"0.0.0.0\"\n")
	port := int32(8086)
	if spec.Server.Port > 0 {
		port = spec.Server.Port
	}
	fmt.Fprintf(b, "port = %d\n", port)
	if spec.Server.MaxBodySizeBytes > 0 {
		fmt.Fprintf(b, "max_body_size_bytes = %d\n", spec.Server.MaxBodySizeBytes)
	}
	if spec.Server.RequestTimeoutSecs > 0 {
		fmt.Fprintf(b, "request_timeout_secs = %d\n", spec.Server.RequestTimeoutSecs)
	}
	if spec.Server.QueryTimeoutSecs > 0 {
		fmt.Fprintf(b, "query_timeout_secs = %d\n", spec.Server.QueryTimeoutSecs)
	}
	if spec.Server.MaxConcurrentQueries > 0 {
		fmt.Fprintf(b, "max_concurrent_queries = %d\n", spec.Server.MaxConcurrentQueries)
	}
	if spec.Server.TLS != nil && spec.Server.TLS.Enabled {
		b.WriteString("tls_enabled = true\n")
		b.WriteString("tls_cert_path = \"/etc/hyperbytedb/tls/tls.crt\"\n")
		b.WriteString("tls_key_path = \"/etc/hyperbytedb/tls/tls.key\"\n")
	}
}

func writeStorageSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[storage]\n")
	fmt.Fprintf(b, "wal_dir = \"%s\"\n", defaultWalDir)
	fmt.Fprintf(b, "meta_dir = \"%s\"\n", defaultMetaDir)
	walFormat := "bincode"
	if spec.Storage.WALFormat != "" {
		walFormat = spec.Storage.WALFormat
	}
	fmt.Fprintf(b, "wal_format = \"%s\"\n", walFormat)
}

func writeFlushSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[flush]\n")
	intervalSecs := int32(10)
	if spec.Flush.IntervalSecs > 0 {
		intervalSecs = spec.Flush.IntervalSecs
	}
	fmt.Fprintf(b, "interval_secs = %d\n", intervalSecs)
	walThreshold := int32(64)
	if spec.Flush.WALSizeThresholdMB > 0 {
		walThreshold = spec.Flush.WALSizeThresholdMB
	}
	fmt.Fprintf(b, "wal_size_threshold_mb = %d\n", walThreshold)
	timeBucket := "1h"
	if spec.Flush.TimeBucketDuration != "" {
		timeBucket = spec.Flush.TimeBucketDuration
	}
	fmt.Fprintf(b, "time_bucket_duration = \"%s\"\n", timeBucket)
	maxPointsPerBatch := int32(50000)
	if spec.Flush.MaxPointsPerBatch > 0 {
		maxPointsPerBatch = spec.Flush.MaxPointsPerBatch
	}
	fmt.Fprintf(b, "max_points_per_batch = %d\n", maxPointsPerBatch)
	if spec.Flush.WALBatchSize > 0 {
		fmt.Fprintf(b, "wal_batch_size = %d\n", spec.Flush.WALBatchSize)
	}
	if spec.Flush.WALBatchDelayUs > 0 {
		fmt.Fprintf(b, "wal_batch_delay_us = %d\n", spec.Flush.WALBatchDelayUs)
	}
	arrowWALEnabled := true
	if spec.Flush.ArrowWALEnabled != nil {
		arrowWALEnabled = *spec.Flush.ArrowWALEnabled
	}
	fmt.Fprintf(b, "arrow_wal_enabled = %t\n", arrowWALEnabled)
}

func writeChdbSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[chdb]\n")
	sessionPath := defaultChdbSessionPath
	if spec.ChDB.SessionDataPath != "" {
		sessionPath = spec.ChDB.SessionDataPath
	}
	fmt.Fprintf(b, "session_data_path = \"%s\"\n", sessionPath)
	if spec.ChDB.QueryPoolSize > 0 {
		fmt.Fprintf(b, "query_pool_size = %d\n", spec.ChDB.QueryPoolSize)
	}
	if spec.ChDB.WritePoolSize > 0 {
		fmt.Fprintf(b, "write_pool_size = %d\n", spec.ChDB.WritePoolSize)
	}
	if spec.ChDB.QueryPoolSize <= 0 && spec.ChDB.WritePoolSize <= 0 {
		poolSize := int32(1)
		if spec.ChDB.PoolSize > 0 {
			poolSize = spec.ChDB.PoolSize
		}
		fmt.Fprintf(b, "pool_size = %d\n", poolSize)
	}
}

func writeAuthSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[auth]\n")
	fmt.Fprintf(b, "enabled = %t\n", spec.Auth.Enabled)
}

func writeCardinalitySection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	maxTag := int64(100_000)
	if spec.Cardinality.MaxTagValuesPerMeasurement > 0 {
		maxTag = spec.Cardinality.MaxTagValuesPerMeasurement
	}
	maxMeas := int64(10_000)
	if spec.Cardinality.MaxMeasurementsPerDatabase > 0 {
		maxMeas = spec.Cardinality.MaxMeasurementsPerDatabase
	}
	b.WriteString("\n[cardinality]\n")
	fmt.Fprintf(b, "max_tag_values_per_measurement = %d\n", maxTag)
	fmt.Fprintf(b, "max_measurements_per_database = %d\n", maxMeas)
}

func writeLoggingSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[logging]\n")
	level := "info"
	if spec.Logging.Level != "" {
		level = spec.Logging.Level
	}
	fmt.Fprintf(b, "level = \"%s\"\n", level)
	format := "text"
	if spec.Logging.Format != "" {
		format = spec.Logging.Format
	}
	fmt.Fprintf(b, "format = \"%s\"\n", format)

}

func writeStatementSummarySection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	enabled := true
	if spec.StatementSummary.Enabled != nil {
		enabled = *spec.StatementSummary.Enabled
	}
	maxEntries := int32(1000)
	if spec.StatementSummary.MaxEntries > 0 {
		maxEntries = spec.StatementSummary.MaxEntries
	}
	b.WriteString("\n[statement_summary]\n")
	fmt.Fprintf(b, "enabled = %t\n", enabled)
	fmt.Fprintf(b, "max_entries = %d\n", maxEntries)
}

func writeHintedHandoffSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	enabled := true
	if spec.HintedHandoff.Enabled != nil {
		enabled = *spec.HintedHandoff.Enabled
	}
	maxHints := int64(100_000)
	if spec.HintedHandoff.MaxHintsPerPeer > 0 {
		maxHints = spec.HintedHandoff.MaxHintsPerPeer
	}
	maxAge := int64(3600)
	if spec.HintedHandoff.MaxHintAgeSecs > 0 {
		maxAge = spec.HintedHandoff.MaxHintAgeSecs
	}
	b.WriteString("\n[hinted_handoff]\n")
	fmt.Fprintf(b, "enabled = %t\n", enabled)
	fmt.Fprintf(b, "max_hints_per_peer = %d\n", maxHints)
	fmt.Fprintf(b, "max_hint_age_secs = %d\n", maxAge)
}

func writeRateLimitSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	if spec.RateLimit.Enabled == nil && spec.RateLimit.MaxRequestsPerSecond == 0 {
		return
	}
	b.WriteString("\n[rate_limit]\n")
	if spec.RateLimit.Enabled != nil {
		fmt.Fprintf(b, "enabled = %t\n", *spec.RateLimit.Enabled)
	}
	if spec.RateLimit.MaxRequestsPerSecond > 0 {
		fmt.Fprintf(b, "max_requests_per_second = %d\n", spec.RateLimit.MaxRequestsPerSecond)
	}
}

func writeRetentionSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	enabled := true
	if spec.Retention.Enabled != nil {
		enabled = *spec.Retention.Enabled
	}
	interval := "12h"
	if spec.Retention.Interval != "" {
		interval = spec.Retention.Interval
	}
	b.WriteString("\n[retention]\n")
	fmt.Fprintf(b, "enabled = %t\n", enabled)
	fmt.Fprintf(b, "interval = \"%s\"\n", interval)
}

func writeClusterSection(b *strings.Builder, cluster *v1alpha1.HyperbytedbCluster, clusterEnabled bool) {
	spec := &cluster.Spec
	b.WriteString("\n[cluster]\n")
	fmt.Fprintf(b, "enabled = %t\n", clusterEnabled)
	b.WriteString("peers = \"\"\n")
	fmt.Fprintf(b, "replication_log_dir = \"%s\"\n", defaultReplLogDir)
	fmt.Fprintf(b, "raft_dir = \"%s\"\n", defaultRaftDir)

	heartbeatInterval := int32(2)
	if spec.Cluster.HeartbeatIntervalSecs > 0 {
		heartbeatInterval = spec.Cluster.HeartbeatIntervalSecs
	}
	fmt.Fprintf(b, "heartbeat_interval_secs = %d\n", heartbeatInterval)

	heartbeatMiss := int32(5)
	if spec.Cluster.HeartbeatMissThreshold > 0 {
		heartbeatMiss = spec.Cluster.HeartbeatMissThreshold
	}
	fmt.Fprintf(b, "heartbeat_miss_threshold = %d\n", heartbeatMiss)

	replRetries := int32(5)
	if spec.Cluster.ReplicationMaxRetries > 0 {
		replRetries = spec.Cluster.ReplicationMaxRetries
	}
	fmt.Fprintf(b, "replication_max_retries = %d\n", replRetries)

	replQueue := int32(8192)
	if spec.Cluster.ReplicationQueueDepth > 0 {
		replQueue = spec.Cluster.ReplicationQueueDepth
	}
	fmt.Fprintf(b, "replication_queue_depth = %d\n", replQueue)

	replInflight := int32(8)
	if spec.Cluster.ReplicationMaxInflightBatches > 0 {
		replInflight = spec.Cluster.ReplicationMaxInflightBatches
	}
	fmt.Fprintf(b, "replication_max_inflight_batches = %d\n", replInflight)

	replCoalesce := int64(8 * 1024 * 1024)
	if spec.Cluster.ReplicationMaxCoalesceBodyBytes > 0 {
		replCoalesce = spec.Cluster.ReplicationMaxCoalesceBodyBytes
	}
	fmt.Fprintf(b, "replication_max_coalesce_body_bytes = %d\n", replCoalesce)

	recvQueue := int32(1024)
	if spec.Cluster.ReplicateReceiverQueueDepth > 0 {
		recvQueue = spec.Cluster.ReplicateReceiverQueueDepth
	}
	fmt.Fprintf(b, "replicate_receiver_queue_depth = %d\n", recvQueue)

	truncateMult := int64(2)
	if spec.Cluster.ReplicationTruncateStalePeerMultiplier > 0 {
		truncateMult = spec.Cluster.ReplicationTruncateStalePeerMultiplier
	}
	fmt.Fprintf(b, "replication_truncate_stale_peer_multiplier = %d\n", truncateMult)

	if spec.Cluster.RaftHeartbeatIntervalMs > 0 {
		fmt.Fprintf(b, "raft_heartbeat_interval_ms = %d\n", spec.Cluster.RaftHeartbeatIntervalMs)
	}
	if spec.Cluster.RaftElectionTimeoutMs > 0 {
		fmt.Fprintf(b, "raft_election_timeout_ms = %d\n", spec.Cluster.RaftElectionTimeoutMs)
	}
	if spec.Cluster.RaftSnapshotThreshold > 0 {
		fmt.Fprintf(b, "raft_snapshot_threshold = %d\n", spec.Cluster.RaftSnapshotThreshold)
	}

	writeReplicationSubsections(b, spec.Cluster.Replication)
}

func writeReplicationSubsections(b *strings.Builder, repl *v1alpha1.ReplicationSpec) {
	if repl == nil {
		return
	}
	b.WriteString("\n[cluster.replication]\n")
	mode := repl.Mode
	if mode == "" {
		mode = "async"
	}
	fmt.Fprintf(b, "mode = \"%s\"\n", mode)
	if repl.AckTimeoutMs > 0 {
		fmt.Fprintf(b, "ack_timeout_ms = %d\n", repl.AckTimeoutMs)
	}
	if repl.SyncQuorum != nil && repl.SyncQuorum.MinAcks != nil {
		b.WriteString("\n[cluster.replication.sync_quorum]\n")
		writeMinAcks(b, repl.SyncQuorum.MinAcks)
	}
}

func writeMinAcks(b *strings.Builder, v *intstr.IntOrString) {
	switch v.Type {
	case intstr.Int:
		fmt.Fprintf(b, "min_acks = %d\n", v.IntValue())
	case intstr.String:
		fmt.Fprintf(b, "min_acks = \"%s\"\n", v.StrVal)
	}
}
