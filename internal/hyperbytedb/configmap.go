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
	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}
	return renderConfigTOMLWithClusterEnabled(cluster, replicas > 1)
}

func renderConfigTOMLWithClusterEnabled(cluster *v1alpha1.HyperbytedbCluster, clusterEnabled bool) string {
	spec := &cluster.Spec
	var b strings.Builder

	writeServerSection(&b, spec)
	writeStorageSection(&b, spec)
	writeFlushSection(&b, spec)
	writeCompactionSection(&b, spec)
	writeChdbSection(&b, spec)
	writeAuthSection(&b, spec)
	writeCardinalitySection(&b, spec)
	writeLoggingSection(&b, spec)
	writeStatementSummarySection(&b, spec)
	writeHintedHandoffSection(&b, spec)
	writeRateLimitSection(&b, spec)
	writeClusterSection(&b, spec, clusterEnabled)

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
	// 0 means unlimited; emit only when caller explicitly set a positive limit.
	if spec.Server.MaxConcurrentQueries > 0 {
		fmt.Fprintf(b, "max_concurrent_queries = %d\n", spec.Server.MaxConcurrentQueries)
	}

	if spec.Server.TLS != nil && spec.Server.TLS.Enabled {
		b.WriteString("\n[server.tls]\n")
		b.WriteString("enabled = true\n")
		b.WriteString("cert_file = \"/etc/hyperbytedb/tls/tls.crt\"\n")
		b.WriteString("key_file = \"/etc/hyperbytedb/tls/tls.key\"\n")
	}
}

func writeStorageSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[storage]\n")
	b.WriteString("data_dir = \"/var/lib/hyperbytedb/data\"\n")
	b.WriteString("wal_dir = \"/var/lib/hyperbytedb/wal\"\n")
	b.WriteString("meta_dir = \"/var/lib/hyperbytedb/meta\"\n")
	backend := "local"
	if spec.Storage.Backend != "" {
		backend = spec.Storage.Backend
	}
	fmt.Fprintf(b, "backend = \"%s\"\n", backend)

	if spec.Storage.S3 != nil {
		b.WriteString("\n[storage.s3]\n")
		fmt.Fprintf(b, "bucket = \"%s\"\n", spec.Storage.S3.Bucket)
		if spec.Storage.S3.Prefix != "" {
			fmt.Fprintf(b, "prefix = \"%s\"\n", spec.Storage.S3.Prefix)
		}
		if spec.Storage.S3.Region != "" {
			fmt.Fprintf(b, "region = \"%s\"\n", spec.Storage.S3.Region)
		}
		if spec.Storage.S3.Endpoint != "" {
			fmt.Fprintf(b, "endpoint = \"%s\"\n", spec.Storage.S3.Endpoint)
		}
	}
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
	if spec.Flush.MaxPointsPerBatch > 0 {
		fmt.Fprintf(b, "max_points_per_batch = %d\n", spec.Flush.MaxPointsPerBatch)
	}
	if spec.Flush.WALBatchSize > 0 {
		fmt.Fprintf(b, "wal_batch_size = %d\n", spec.Flush.WALBatchSize)
	}
	if spec.Flush.WALBatchDelayUs > 0 {
		fmt.Fprintf(b, "wal_batch_delay_us = %d\n", spec.Flush.WALBatchDelayUs)
	}
}

func writeCompactionSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[compaction]\n")
	compactionEnabled := true
	if spec.Compaction.Enabled != nil {
		compactionEnabled = *spec.Compaction.Enabled
	}
	fmt.Fprintf(b, "enabled = %t\n", compactionEnabled)
	compactionInterval := int32(300)
	if spec.Compaction.IntervalSecs > 0 {
		compactionInterval = spec.Compaction.IntervalSecs
	}
	fmt.Fprintf(b, "interval_secs = %d\n", compactionInterval)
	minFiles := int32(4)
	if spec.Compaction.MinFilesToCompact > 0 {
		minFiles = spec.Compaction.MinFilesToCompact
	}
	fmt.Fprintf(b, "min_files_to_compact = %d\n", minFiles)
	targetSize := int32(256)
	if spec.Compaction.TargetFileSizeMB > 0 {
		targetSize = spec.Compaction.TargetFileSizeMB
	}
	fmt.Fprintf(b, "target_file_size_mb = %d\n", targetSize)
	if spec.Compaction.BucketDuration != "" {
		fmt.Fprintf(b, "bucket_duration = \"%s\"\n", spec.Compaction.BucketDuration)
	}
	if spec.Compaction.VerifiedCompactionAgeSecs > 0 {
		fmt.Fprintf(b, "verified_compaction_age_secs = %d\n", spec.Compaction.VerifiedCompactionAgeSecs)
	}
	if spec.Compaction.SelfRepairEnabled != nil {
		fmt.Fprintf(b, "self_repair_enabled = %t\n", *spec.Compaction.SelfRepairEnabled)
	}
	if spec.Compaction.MaxRepairChecksPerCycle > 0 {
		fmt.Fprintf(b, "max_repair_checks_per_cycle = %d\n", spec.Compaction.MaxRepairChecksPerCycle)
	}
	if spec.Compaction.CompactAllMaxInflight > 0 {
		fmt.Fprintf(b, "compact_all_max_inflight = %d\n", spec.Compaction.CompactAllMaxInflight)
	}
}

func writeChdbSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[chdb]\n")
	b.WriteString("session_data_path = \"/var/lib/hyperbytedb/chdb\"\n")
	poolSize := int32(4)
	if spec.ChDB.PoolSize > 0 {
		poolSize = spec.ChDB.PoolSize
	}
	fmt.Fprintf(b, "pool_size = %d\n", poolSize)
}

func writeAuthSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	b.WriteString("\n[auth]\n")
	fmt.Fprintf(b, "enabled = %t\n", spec.Auth.Enabled)
}

func writeCardinalitySection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	if spec.Cardinality.MaxTagValuesPerMeasurement == 0 && spec.Cardinality.MaxMeasurementsPerDatabase == 0 {
		return
	}
	b.WriteString("\n[cardinality]\n")
	if spec.Cardinality.MaxTagValuesPerMeasurement > 0 {
		fmt.Fprintf(b, "max_tag_values_per_measurement = %d\n", spec.Cardinality.MaxTagValuesPerMeasurement)
	}
	if spec.Cardinality.MaxMeasurementsPerDatabase > 0 {
		fmt.Fprintf(b, "max_measurements_per_database = %d\n", spec.Cardinality.MaxMeasurementsPerDatabase)
	}
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
	if spec.StatementSummary.Enabled == nil && spec.StatementSummary.MaxEntries == 0 {
		return
	}
	b.WriteString("\n[statement_summary]\n")
	if spec.StatementSummary.Enabled != nil {
		fmt.Fprintf(b, "enabled = %t\n", *spec.StatementSummary.Enabled)
	}
	if spec.StatementSummary.MaxEntries > 0 {
		fmt.Fprintf(b, "max_entries = %d\n", spec.StatementSummary.MaxEntries)
	}
}

func writeHintedHandoffSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec) {
	if spec.HintedHandoff.Enabled == nil && spec.HintedHandoff.MaxHintsPerPeer == 0 && spec.HintedHandoff.MaxHintAgeSecs == 0 {
		return
	}
	b.WriteString("\n[hinted_handoff]\n")
	if spec.HintedHandoff.Enabled != nil {
		fmt.Fprintf(b, "enabled = %t\n", *spec.HintedHandoff.Enabled)
	}
	if spec.HintedHandoff.MaxHintsPerPeer > 0 {
		fmt.Fprintf(b, "max_hints_per_peer = %d\n", spec.HintedHandoff.MaxHintsPerPeer)
	}
	if spec.HintedHandoff.MaxHintAgeSecs > 0 {
		fmt.Fprintf(b, "max_hint_age_secs = %d\n", spec.HintedHandoff.MaxHintAgeSecs)
	}
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

func writeClusterSection(b *strings.Builder, spec *v1alpha1.HyperbytedbClusterSpec, clusterEnabled bool) {
	b.WriteString("\n[cluster]\n")
	fmt.Fprintf(b, "enabled = %t\n", clusterEnabled)
	b.WriteString("replication_log_dir = \"/var/lib/hyperbytedb/replication_log\"\n")

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

	aeEnabled := true
	if spec.Cluster.AntiEntropyEnabled != nil {
		aeEnabled = *spec.Cluster.AntiEntropyEnabled
	}
	fmt.Fprintf(b, "anti_entropy_enabled = %t\n", aeEnabled)

	aeInterval := int32(60)
	if spec.Cluster.AntiEntropyIntervalSecs > 0 {
		aeInterval = spec.Cluster.AntiEntropyIntervalSecs
	}
	fmt.Fprintf(b, "anti_entropy_interval_secs = %d\n", aeInterval)

	syncFiles := int32(4)
	if spec.Cluster.SyncMaxConcurrentFiles > 0 {
		syncFiles = spec.Cluster.SyncMaxConcurrentFiles
	}
	fmt.Fprintf(b, "sync_max_concurrent_files = %d\n", syncFiles)

	replRetries := int32(5)
	if spec.Cluster.ReplicationMaxRetries > 0 {
		replRetries = spec.Cluster.ReplicationMaxRetries
	}
	fmt.Fprintf(b, "replication_max_retries = %d\n", replRetries)

	if spec.Cluster.ReplicationQueueDepth > 0 {
		fmt.Fprintf(b, "replication_queue_depth = %d\n", spec.Cluster.ReplicationQueueDepth)
	}
	if spec.Cluster.ReplicationMaxInflightBatches > 0 {
		fmt.Fprintf(b, "replication_max_inflight_batches = %d\n", spec.Cluster.ReplicationMaxInflightBatches)
	}
	if spec.Cluster.ReplicationMaxCoalesceBodyBytes > 0 {
		fmt.Fprintf(b, "replication_max_coalesce_body_bytes = %d\n", spec.Cluster.ReplicationMaxCoalesceBodyBytes)
	}
	if spec.Cluster.ReplicateReceiverQueueDepth > 0 {
		fmt.Fprintf(b, "replicate_receiver_queue_depth = %d\n", spec.Cluster.ReplicateReceiverQueueDepth)
	}
	if spec.Cluster.ReplicationTruncateStalePeerMultiplier > 0 {
		fmt.Fprintf(b, "replication_truncate_stale_peer_multiplier = %d\n", spec.Cluster.ReplicationTruncateStalePeerMultiplier)
	}

	raftHB := int32(300)
	if spec.Cluster.RaftHeartbeatIntervalMs > 0 {
		raftHB = spec.Cluster.RaftHeartbeatIntervalMs
	}
	fmt.Fprintf(b, "raft_heartbeat_interval_ms = %d\n", raftHB)

	raftElection := int32(1000)
	if spec.Cluster.RaftElectionTimeoutMs > 0 {
		raftElection = spec.Cluster.RaftElectionTimeoutMs
	}
	fmt.Fprintf(b, "raft_election_timeout_ms = %d\n", raftElection)

	raftSnapshot := int32(1000)
	if spec.Cluster.RaftSnapshotThreshold > 0 {
		raftSnapshot = spec.Cluster.RaftSnapshotThreshold
	}
	fmt.Fprintf(b, "raft_snapshot_threshold = %d\n", raftSnapshot)

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
		// Quote string forms (e.g. "majority") so TOML treats them as strings.
		fmt.Fprintf(b, "min_acks = \"%s\"\n", v.StrVal)
	}
}
