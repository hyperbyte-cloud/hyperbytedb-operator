package hyperbytedb

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func proxyCluster(version, proxyImage string, proxyPort int32) *v1alpha1.HyperbytedbCluster {
	c := versionedCluster(version, "", nil)
	c.Name = "shared-pool-1"
	c.Spec.Proxy = &v1alpha1.ProxySpec{Enabled: true, Image: proxyImage, Port: proxyPort}
	return c
}

func proxyContainer(t *testing.T, cluster *v1alpha1.HyperbytedbCluster) corev1.Container {
	t.Helper()
	cs := BuildProxyDeployment(cluster).Spec.Template.Spec.Containers
	if len(cs) != 1 {
		t.Fatalf("want one proxy container, got %d", len(cs))
	}
	return cs[0]
}

func envValue(c corev1.Container, name string) (string, bool) {
	for _, e := range c.Env {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}

func portNumber(c corev1.Container, name string) int32 {
	for _, p := range c.Ports {
		if p.Name == name {
			return p.ContainerPort
		}
	}
	return 0
}

// Proxy 0.9.0-beta and later serve /healthz and /readyz only on their admin
// listener and answer 404 on the client port. Probing the client port kept
// shared-pool-1's 0.9.1-beta proxy restarting and never ready, so every
// request through it failed with 502.
func TestA091ProxyIsProbedOnItsAdminListener(t *testing.T) {
	c := proxyContainer(t, proxyCluster("v0.9.1-beta", "ghcr.io/hyperbyte-cloud/hyperbytedb-proxy", 0))

	if got := c.LivenessProbe.HTTPGet.Port.String(); got != proxyAdminPortName {
		t.Errorf("liveness probe port = %s, want %s", got, proxyAdminPortName)
	}
	if got := c.ReadinessProbe.HTTPGet.Port.String(); got != proxyAdminPortName {
		t.Errorf("readiness probe port = %s, want %s", got, proxyAdminPortName)
	}
	if got := portNumber(c, proxyAdminPortName); got != 8087 {
		t.Errorf("admin container port = %d, want 8087", got)
	}
	if v, ok := envValue(c, "HYPERBYTEDB_PROXY_ADMIN_LISTEN"); !ok || v != "0.0.0.0:8087" {
		t.Errorf("HYPERBYTEDB_PROXY_ADMIN_LISTEN = %q (set %v), want 0.0.0.0:8087", v, ok)
	}
	if got := portNumber(c, httpPortName); got != 8086 {
		t.Errorf("client port = %d, want 8086", got)
	}
}

// A 0.8.5 proxy serves everything on one port: its pod is rendered as before,
// so upgrading the operator does not restart it.
func TestAn085ProxyKeepsItsProbesOnTheClientPort(t *testing.T) {
	c := proxyContainer(t, proxyCluster("0.8.5-beta", "", 0))

	if got := c.LivenessProbe.HTTPGet.Port.String(); got != httpPortName {
		t.Errorf("liveness probe port = %s, want %s", got, httpPortName)
	}
	if got := c.ReadinessProbe.HTTPGet.Port.String(); got != httpPortName {
		t.Errorf("readiness probe port = %s, want %s", got, httpPortName)
	}
	if _, ok := envValue(c, "HYPERBYTEDB_PROXY_ADMIN_LISTEN"); ok {
		t.Error("a 0.8.5 proxy got an admin listener")
	}
	if len(c.Ports) != 1 {
		t.Errorf("ports = %v, want only the client port", c.Ports)
	}
}

// The proxy's version is its image's: a proxy image pinned to its own tag
// wins over the cluster's version, and an unparseable tag falls back to it.
func TestTheProxyVersionFollowsItsOwnImageTag(t *testing.T) {
	cases := []struct {
		name, version, image string
		admin                bool
	}{
		{"follows spec.version", "0.9.1-beta", "", true},
		{"old proxy pinned on a new cluster", "0.9.1-beta", "ghcr.io/hyperbyte-cloud/hyperbytedb-proxy:0.8.5-beta", false},
		{"new proxy pinned on an old cluster", "0.8.5-beta", "ghcr.io/hyperbyte-cloud/hyperbytedb-proxy:0.9.1-beta", true},
		{"unparseable tag follows the database", "0.8.5-beta", "registry.local/proxy:e2e", false},
	}
	for _, tc := range cases {
		got := ProxySupports(proxyCluster(tc.version, tc.image, 0), CapabilityProxyAdminListener)
		if got != tc.admin {
			t.Errorf("%s: admin listener = %v, want %v", tc.name, got, tc.admin)
		}
	}
}

// The operator calls each proxy pod's admin API (exclude / include during
// rolling restarts) on ProxyAdminPort: the admin listener for 0.9 proxies,
// the proxy's own client port before. It used the database's port, which
// is only right when spec.proxy.port is unset.
func TestTheProxyAdminPort(t *testing.T) {
	if got := ProxyAdminPort(proxyCluster("0.9.1-beta", "", 0)); got != 8087 {
		t.Errorf("0.9.1 proxy: %d, want 8087", got)
	}
	if got := ProxyAdminPort(proxyCluster("0.8.5-beta", "", 9000)); got != 9000 {
		t.Errorf("0.8.5 proxy on port 9000: %d, want 9000", got)
	}
	// A client port of 8087 moves the admin listener off it.
	c := proxyCluster("0.9.1-beta", "", 8087)
	if got := ProxyAdminPort(c); got != 8088 {
		t.Errorf("0.9.1 proxy on client port 8087: admin %d, want 8088", got)
	}
	pc := proxyContainer(t, c)
	if portNumber(pc, httpPortName) == portNumber(pc, proxyAdminPortName) {
		t.Errorf("client and admin ports collide: %v", pc.Ports)
	}
}

// The admin port is not on the client Service: /admin can exclude backends.
func TestTheProxyServiceExposesOnlyTheClientPort(t *testing.T) {
	svc := BuildProxyService(proxyCluster("0.9.1-beta", "", 0))
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Name != httpPortName {
		t.Errorf("service ports = %v, want only %s", svc.Spec.Ports, httpPortName)
	}
}

// Exclude is asked again after a partial failure: a proxy that already did it
// answers 409, and one that has no such backend answers its own 404. Both
// are done; any other 404 (a wrong port or path, as v0.3.0's) is not.
func TestExcludeTreatsAlreadyDoneAsDone(t *testing.T) {
	cases := []struct {
		status int
		body   string
		ok     bool
	}{
		{http.StatusOK, `{"status":"excluded"}`, true},
		{http.StatusConflict, `{"status":"already_excluded"}`, true},
		{http.StatusNotFound, `{"status":"error","message":"backend 10.0.0.9 not found in pool"}`, true},
		{http.StatusNotFound, `not found`, false},
		{http.StatusInternalServerError, `boom`, false},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
		port, _ := strconv.Atoi(portStr)
		err := NewClient().ExcludeProxyBackend(context.Background(), host, int32(port), "10.0.0.9")
		if (err == nil) != tc.ok {
			t.Errorf("%d %q: err = %v, want ok=%v", tc.status, tc.body, err, tc.ok)
		}
		srv.Close()
	}
}
