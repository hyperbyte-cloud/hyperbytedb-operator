package hyperbytedb

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func TestRenderConfigTOML_singleNode(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(1)),
			Server: v1alpha1.ServerSpec{
				Port: 9090,
			},
		},
	}

	out := renderConfigTOML(cluster)

	for _, want := range []string{
		"[server]",
		"port = 9090",
		"[storage]",
		`wal_dir = "/var/lib/hyperbytedb/wal"`,
		`meta_dir = "/var/lib/hyperbytedb/meta"`,
		"[cluster]",
		"enabled = false",
		"[retention]",
		`interval = "12h"`,
		`wal_format = "bincode"`,
		"arrow_wal_enabled = true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected config to contain %q\n\ngot:\n%s", want, out)
		}
	}

	for _, absent := range []string{
		"[compaction]",
		"[server.tls]",
		"data_dir",
		"anti_entropy",
		"wal_replication",
		"[sharding]",
	} {
		if strings.Contains(out, absent) {
			t.Fatalf("expected config NOT to contain %q\n\ngot:\n%s", absent, out)
		}
	}
}

func TestRenderConfigTOML_clusterAndTLS(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(3)),
			Server: v1alpha1.ServerSpec{
				TLS: &v1alpha1.TLSSpec{Enabled: true},
			},
			Cluster: v1alpha1.ClusterTuningSpec{
				Replication: &v1alpha1.ReplicationSpec{
					Mode:         "sync_quorum",
					AckTimeoutMs: 3000,
					SyncQuorum: &v1alpha1.SyncQuorumSpec{
						MinAcks: ptr.To(intstr.FromString("majority")),
					},
				},
			},
			Retention: v1alpha1.RetentionSpec{
				Enabled:  ptr.To(false),
				Interval: "5m",
			},
		},
	}

	out := renderConfigTOML(cluster)

	for _, want := range []string{
		"enabled = true",
		"tls_enabled = true",
		`tls_cert_path = "/etc/hyperbytedb/tls/tls.crt"`,
		`tls_key_path = "/etc/hyperbytedb/tls/tls.key"`,
		`mode = "sync_quorum"`,
		"ack_timeout_ms = 3000",
		`min_acks = "majority"`,
		"enabled = false",
		`interval = "5m"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected config to contain %q\n\ngot:\n%s", want, out)
		}
	}
}

func TestConfigHash_ignoresReplicaCount(t *testing.T) {
	base := v1alpha1.HyperbytedbClusterSpec{
		Server: v1alpha1.ServerSpec{Port: 8086},
	}
	one := &v1alpha1.HyperbytedbCluster{Spec: base}
	one.Spec.Replicas = ptr.To(int32(1))
	three := &v1alpha1.HyperbytedbCluster{Spec: base}
	three.Spec.Replicas = ptr.To(int32(3))

	if ConfigHash(one) != ConfigHash(three) {
		t.Fatal("ConfigHash must not change when only replica count changes")
	}
}

func TestRenderConfigTOML_oneReplicaExplicitShardingIsCluster(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(1)),
			Sharding: &v1alpha1.ShardingSpec{Enabled: true},
		},
	}
	out := renderConfigTOML(cluster)
	if !strings.Contains(out, "[cluster]") {
		t.Fatal("expected [cluster] section")
	}
	// First `enabled = true` in the file is [cluster] (sharding follows).
	clusterIdx := strings.Index(out, "[cluster]")
	shardIdx := strings.Index(out, "[sharding]")
	if clusterIdx < 0 || shardIdx < 0 || shardIdx < clusterIdx {
		t.Fatalf("expected [cluster] then [sharding]\n%s", out)
	}
	clusterBlock := out[clusterIdx:shardIdx]
	if !strings.Contains(clusterBlock, "enabled = true") {
		t.Fatalf("1-replica explicit sharding must set [cluster] enabled=true\n%s", clusterBlock)
	}
	if !strings.Contains(out[shardIdx:], "enabled = true") {
		t.Fatalf("expected sharding.enabled=true\n%s", out)
	}
}

func TestRenderConfigTOML_sharding(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(3)),
			Sharding: &v1alpha1.ShardingSpec{
				Enabled:                  true,
				ReplicationFactor:        2,
				RegionSplitSeries:        5,
				RegionMaxSeries:          10,
				RegionMergeSeries:        2,
				HeartbeatIntervalSecs:    10,
				LoadSplitQpsThreshold:    ptr.To(int64(0)),
				MaxRegionsPerMeasurement: 128,
			},
		},
	}

	out := renderConfigTOML(cluster)
	for _, want := range []string{
		"[sharding]",
		"enabled = true",
		"replication_factor = 2",
		"region_split_series = 5",
		"region_max_series = 10",
		"region_merge_series = 2",
		"heartbeat_interval_secs = 10",
		"load_split_qps_threshold = 0",
		"max_regions_per_measurement = 128",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected config to contain %q\n\ngot:\n%s", want, out)
		}
	}

	off := &v1alpha1.HyperbytedbCluster{Spec: cluster.Spec}
	off.Spec.Sharding = &v1alpha1.ShardingSpec{Enabled: false}
	if ConfigHash(cluster) == ConfigHash(off) {
		t.Fatal("ConfigHash must change when sharding.enabled changes")
	}
}
