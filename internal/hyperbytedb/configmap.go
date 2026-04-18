package hyperbytedb

import (
	"crypto/sha256"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
// Replica count must not affect this hash: scaling up/down updates the mounted ConfigMap and
// peers ConfigMap without recycling existing pods. The live config.toml still sets
// [cluster].enabled from replica count; only this digest ignores that toggle.
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

	b.WriteString("[server]\n")
	b.WriteString("bind_address = \"0.0.0.0\"\n")
	port := int32(8086)
	if spec.Server.Port > 0 {
		port = spec.Server.Port
	}
	b.WriteString(fmt.Sprintf("port = %d\n", port))
	if spec.Server.MaxBodySizeBytes > 0 {
		b.WriteString(fmt.Sprintf("max_body_size_bytes = %d\n", spec.Server.MaxBodySizeBytes))
	}
	if spec.Server.RequestTimeoutSecs > 0 {
		b.WriteString(fmt.Sprintf("request_timeout_secs = %d\n", spec.Server.RequestTimeoutSecs))
	}
	if spec.Server.QueryTimeoutSecs > 0 {
		b.WriteString(fmt.Sprintf("query_timeout_secs = %d\n", spec.Server.QueryTimeoutSecs))
	}

	if spec.Server.TLS != nil && spec.Server.TLS.Enabled {
		b.WriteString("\n[server.tls]\n")
		b.WriteString("enabled = true\n")
		b.WriteString("cert_file = \"/etc/hyperbytedb/tls/tls.crt\"\n")
		b.WriteString("key_file = \"/etc/hyperbytedb/tls/tls.key\"\n")
	}

	b.WriteString("\n[storage]\n")
	b.WriteString("data_dir = \"/var/lib/hyperbytedb/data\"\n")
	b.WriteString("wal_dir = \"/var/lib/hyperbytedb/wal\"\n")
	b.WriteString("meta_dir = \"/var/lib/hyperbytedb/meta\"\n")
	backend := "local"
	if spec.Storage.Backend != "" {
		backend = spec.Storage.Backend
	}
	b.WriteString(fmt.Sprintf("backend = \"%s\"\n", backend))

	if spec.Storage.S3 != nil {
		b.WriteString("\n[storage.s3]\n")
		b.WriteString(fmt.Sprintf("bucket = \"%s\"\n", spec.Storage.S3.Bucket))
		if spec.Storage.S3.Prefix != "" {
			b.WriteString(fmt.Sprintf("prefix = \"%s\"\n", spec.Storage.S3.Prefix))
		}
		if spec.Storage.S3.Region != "" {
			b.WriteString(fmt.Sprintf("region = \"%s\"\n", spec.Storage.S3.Region))
		}
		if spec.Storage.S3.Endpoint != "" {
			b.WriteString(fmt.Sprintf("endpoint = \"%s\"\n", spec.Storage.S3.Endpoint))
		}
	}

	b.WriteString("\n[flush]\n")
	intervalSecs := int32(10)
	if spec.Flush.IntervalSecs > 0 {
		intervalSecs = spec.Flush.IntervalSecs
	}
	b.WriteString(fmt.Sprintf("interval_secs = %d\n", intervalSecs))
	walThreshold := int32(64)
	if spec.Flush.WALSizeThresholdMB > 0 {
		walThreshold = spec.Flush.WALSizeThresholdMB
	}
	b.WriteString(fmt.Sprintf("wal_size_threshold_mb = %d\n", walThreshold))
	timeBucket := "1h"
	if spec.Flush.TimeBucketDuration != "" {
		timeBucket = spec.Flush.TimeBucketDuration
	}
	b.WriteString(fmt.Sprintf("time_bucket_duration = \"%s\"\n", timeBucket))

	b.WriteString("\n[compaction]\n")
	compactionEnabled := true
	if spec.Compaction.Enabled != nil {
		compactionEnabled = *spec.Compaction.Enabled
	}
	b.WriteString(fmt.Sprintf("enabled = %t\n", compactionEnabled))
	compactionInterval := int32(300)
	if spec.Compaction.IntervalSecs > 0 {
		compactionInterval = spec.Compaction.IntervalSecs
	}
	b.WriteString(fmt.Sprintf("interval_secs = %d\n", compactionInterval))
	minFiles := int32(4)
	if spec.Compaction.MinFilesToCompact > 0 {
		minFiles = spec.Compaction.MinFilesToCompact
	}
	b.WriteString(fmt.Sprintf("min_files_to_compact = %d\n", minFiles))
	targetSize := int32(256)
	if spec.Compaction.TargetFileSizeMB > 0 {
		targetSize = spec.Compaction.TargetFileSizeMB
	}
	b.WriteString(fmt.Sprintf("target_file_size_mb = %d\n", targetSize))

	b.WriteString("\n[chdb]\n")
	b.WriteString("session_data_path = \"/var/lib/hyperbytedb/chdb\"\n")
	poolSize := int32(4)
	if spec.ChDB.PoolSize > 0 {
		poolSize = spec.ChDB.PoolSize
	}
	b.WriteString(fmt.Sprintf("pool_size = %d\n", poolSize))

	b.WriteString("\n[auth]\n")
	b.WriteString(fmt.Sprintf("enabled = %t\n", spec.Auth.Enabled))

	b.WriteString("\n[logging]\n")
	level := "info"
	if spec.Logging.Level != "" {
		level = spec.Logging.Level
	}
	b.WriteString(fmt.Sprintf("level = \"%s\"\n", level))
	format := "text"
	if spec.Logging.Format != "" {
		format = spec.Logging.Format
	}
	b.WriteString(fmt.Sprintf("format = \"%s\"\n", format))

	writeClusterSection(&b, spec, clusterEnabled)

	return b.String()
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
}
