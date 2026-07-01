package hyperbytedb

import (
	"testing"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func TestNormalizeVersionTag(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "latest"},
		{"0.8.3", "0.8.3"},
		{"v0.8.3", "0.8.3"},
		{"  1.0.0  ", "1.0.0"},
	}
	for _, tc := range tests {
		if got := NormalizeVersionTag(tc.in); got != tc.want {
			t.Errorf("NormalizeVersionTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveHyperbytedbImage(t *testing.T) {
	cluster := func(image, version string) *v1alpha1.HyperbytedbCluster {
		return &v1alpha1.HyperbytedbCluster{
			Spec: v1alpha1.HyperbytedbClusterSpec{
				Image:   image,
				Version: version,
			},
		}
	}

	tests := []struct {
		name string
		c    *v1alpha1.HyperbytedbCluster
		want string
	}{
		{"version only", cluster("", "0.8.3"), "hyperbytedb:0.8.3"},
		{"explicit tag wins", cluster("hyperbytedb:local", "0.8.3"), "hyperbytedb:local"},
		{"repo plus version", cluster("ghcr.io/org/hyperbytedb", "0.8.3"), "ghcr.io/org/hyperbytedb:0.8.3"},
		{"default latest", cluster("", ""), "hyperbytedb:latest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveHyperbytedbImage(tc.c); got != tc.want {
				t.Errorf("ResolveHyperbytedbImage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveProxyImage(t *testing.T) {
	c := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Version: "0.8.3",
			Proxy:   &v1alpha1.ProxySpec{},
		},
	}
	if got := ResolveProxyImage(c); got != "ghcr.io/hyperbyte-cloud/hyperbytedb-proxy:0.8.3" {
		t.Fatalf("ResolveProxyImage() = %q, want ghcr.io/hyperbyte-cloud/hyperbytedb-proxy:0.8.3", got)
	}

	c.Spec.Proxy.Image = "hyperbytedb-proxy:local"
	if got := ResolveProxyImage(c); got != "hyperbytedb-proxy:local" {
		t.Fatalf("explicit proxy image = %q, want hyperbytedb-proxy:local", got)
	}
}
