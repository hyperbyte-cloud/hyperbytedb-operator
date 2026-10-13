package hyperbytedb

import (
	"strings"
	"testing"

	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func mainContainerOf(t *testing.T, cluster *v1alpha1.HyperbytedbCluster) (livenessPort string, ports []string) {
	t.Helper()
	sts := BuildStatefulSet(cluster, "hash")
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name != "hyperbytedb" {
			continue
		}
		for _, p := range c.Ports {
			ports = append(ports, p.Name)
		}
		return c.LivenessProbe.HTTPGet.Port.String(), ports
	}
	t.Fatal("no hyperbytedb container")
	return "", nil
}

// Liveness must use the control listener on a clustered node: on the main
// port, /ping queued behind write work past the 3s timeout and kubelet killed
// busy but healthy nodes.
func TestClusteredLivenessUsesTheControlPort(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(3))},
	}
	cluster.Annotations = map[string]string{ControlListenerAnnotation: "true"}
	port, ports := mainContainerOf(t, cluster)
	if port != "control" {
		t.Errorf("liveness port = %q, want control", port)
	}
	if strings.Join(ports, ",") != "http,control" {
		t.Errorf("container ports = %v, want [http control]", ports)
	}
	toml := renderConfigTOML(cluster)
	if !strings.Contains(toml, "control_port = 8087\n") {
		t.Errorf("config lacks control_port = 8087:\n%s", toml)
	}
}

// A single node has no control listener; nothing changes for it.
func TestSingleNodeKeepsLivenessOnTheMainPort(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(1))},
	}
	port, ports := mainContainerOf(t, cluster)
	if port != "http" || strings.Join(ports, ",") != "http" {
		t.Errorf("single node: liveness %q ports %v, want http / [http]", port, ports)
	}
	if strings.Contains(renderConfigTOML(cluster), "control_port") {
		t.Error("single node config must not set control_port")
	}
}

// Opt-in: an existing cluster on a database build without the control
// listener must keep liveness on the main port. Defaulting it on left a
// 0.8.5-beta cluster restarting in a loop.
func TestClusteredLivenessStaysOnTheMainPortWithoutOptIn(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(3))},
	}
	port, ports := mainContainerOf(t, cluster)
	if port != "http" || strings.Join(ports, ",") != "http" {
		t.Errorf("no opt-in: liveness %q ports %v, want http / [http]", port, ports)
	}
	if strings.Contains(renderConfigTOML(cluster), "control_port") {
		t.Error("no opt-in: config must not set control_port")
	}
}
