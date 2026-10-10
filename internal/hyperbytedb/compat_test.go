package hyperbytedb

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func versionedCluster(version, image string, annotations map[string]string) *v1alpha1.HyperbytedbCluster {
	return &v1alpha1.HyperbytedbCluster{
		ObjectMeta: metav1.ObjectMeta{Annotations: annotations},
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(3)),
			Version:  version,
			Image:    image,
			Sharding: &v1alpha1.ShardingSpec{Enabled: true},
		},
	}
}

func TestResolveDatabaseVersion_precedence(t *testing.T) {
	cases := []struct {
		name                 string
		cluster              *v1alpha1.HyperbytedbCluster
		wantVersion, wantSrc string
	}{
		{"annotation wins", versionedCluster("0.8.5-beta", "", map[string]string{DatabaseVersionAnnotation: "0.9.0-beta"}), "0.9.0-beta", "annotation"},
		{"spec.version, v prefix dropped", versionedCluster("v0.8.5-beta", "", nil), "0.8.5-beta", "spec.version"},
		{"image tag when version is not one", versionedCluster("e2e", "ghcr.io/x/hyperbytedb:0.8.4-beta", nil), "0.8.4-beta", "image tag"},
		{"registry port is not a tag", versionedCluster("", "localhost:5000/hyperbytedb", nil), LatestDatabaseVersion, "assumed"},
		{"dev tag is assumed latest", versionedCluster("e2e", "docker.io/library/hyperbytedb", nil), LatestDatabaseVersion, "assumed"},
	}
	for _, c := range cases {
		got := ResolveDatabaseVersion(c.cluster)
		if got.String() != c.wantVersion || got.Source != c.wantSrc {
			t.Errorf("%s: got %s from %s, want %s from %s", c.name, got, got.Source, c.wantVersion, c.wantSrc)
		}
	}
}

func TestSupports_followsTheMatrix(t *testing.T) {
	for _, version := range []string{"0.8.0", "0.8.5", "0.8.5-beta"} {
		c := versionedCluster(version, "", nil)
		for _, e := range CompatibilityMatrix {
			if Supports(c, e.Capability) {
				t.Errorf("%s must not support %s", version, e.Capability)
			}
		}
	}
	for _, version := range []string{"0.9.0-beta", "0.9.0-beta.2", "0.9.0", "1.2.0"} {
		c := versionedCluster(version, "", nil)
		for _, e := range CompatibilityMatrix {
			if !Supports(c, e.Capability) {
				t.Errorf("%s must support %s", version, e.Capability)
			}
		}
	}
}

// A 0.8 database ignores keys it does not know and runs on its defaults, so
// it must get the keys it reads; a 0.9 one must not get a fixed query pool.
func TestRenderConfigTOML_perDatabaseVersion(t *testing.T) {
	old := versionedCluster("0.8.5-beta", "", map[string]string{
		ControlListenerAnnotation:  "true",
		EvacuateTogetherAnnotation: "true",
	})
	old.Spec.ChDB = v1alpha1.ChDBSpec{QueryPoolSize: 4}
	out := renderConfigTOML(old)
	for _, want := range []string{"\npool_size = 4\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("0.8.5: expected %q\n%s", want, out)
		}
	}
	for _, absent := range []string{"query_pool_size", "write_pool_size", "[sharding]", "control_port"} {
		if strings.Contains(out, absent) {
			t.Errorf("0.8.5: expected no %q\n%s", absent, out)
		}
	}
	if EvacuateTogether(old) || ControlListenerEnabled(old, true) {
		t.Error("0.8.5: the annotations must be ignored")
	}

	cur := versionedCluster("0.9.0-beta", "", map[string]string{ControlListenerAnnotation: "true"})
	out = renderConfigTOML(cur)
	for _, want := range []string{"write_pool_size = 1\n", "[sharding]", "control_port = "} {
		if !strings.Contains(out, want) {
			t.Errorf("0.9.0-beta: expected %q\n%s", want, out)
		}
	}
	for _, absent := range []string{"query_pool_size", "\npool_size"} {
		if strings.Contains(out, absent) {
			t.Errorf("0.9.0-beta: expected no %q (the database sizes it)\n%s", absent, out)
		}
	}
}

func TestCompatibilitySummary_reasons(t *testing.T) {
	cases := []struct {
		cluster *v1alpha1.HyperbytedbCluster
		reason  string
		mention string
	}{
		{versionedCluster("0.9.0-beta", "", nil), "Supported", "HyperbyteDB 0.9.0-beta (from spec.version)"},
		{versionedCluster("0.8.5-beta", "", nil), "Supported", "not rendered for this version: split-chdb-pools"},
		{versionedCluster("0.7.0", "", nil), "OlderThanSupported", "0.7.0"},
		{versionedCluster("2.0.0", "", nil), "NewerThanKnown", "2.0.0"},
		{versionedCluster("e2e", "", nil), "Assumed", DatabaseVersionAnnotation},
	}
	for _, c := range cases {
		reason, message := CompatibilitySummary(c.cluster)
		if reason != c.reason || !strings.Contains(message, c.mention) {
			t.Errorf("%s: got %s %q, want %s mentioning %q", c.cluster.Spec.Version, reason, message, c.reason, c.mention)
		}
	}
}
