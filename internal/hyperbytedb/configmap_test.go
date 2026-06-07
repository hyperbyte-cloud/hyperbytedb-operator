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
		`interval = "60s"`,
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
