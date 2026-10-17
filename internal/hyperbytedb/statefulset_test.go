package hyperbytedb

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func podEnv(t *testing.T, cluster *v1alpha1.HyperbytedbCluster, name string) (string, bool) {
	t.Helper()
	sts := BuildStatefulSet(cluster, "hash")
	for _, c := range sts.Spec.Template.Spec.Containers {
		if c.Name != containerName {
			continue
		}
		for _, e := range c.Env {
			if e.Name == name {
				return e.Value, true
			}
		}
	}
	return "", false
}

// The image bakes HYPERBYTEDB__LOGGING__LEVEL=info and the HYPERBYTEDB__* env
// overlay is applied OVER the mounted config.toml. Rendering the level into the
// ConfigMap alone was therefore inert: spec.logging.level had no effect, and a
// sharded cluster could not be put into debug to diagnose a placement or
// staging failure. The env var is the only thing that can win.
func TestLoggingLevelReachesThePodEnv(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{
			Replicas: ptr.To(int32(3)),
			Logging:  v1alpha1.LoggingSpec{Level: "debug"},
		},
	}
	got, ok := podEnv(t, cluster, "HYPERBYTEDB__LOGGING__LEVEL")
	if !ok {
		t.Fatal("HYPERBYTEDB__LOGGING__LEVEL absent from pod env; the image default would silently win")
	}
	if got != "debug" {
		t.Fatalf("HYPERBYTEDB__LOGGING__LEVEL = %q, want %q", got, "debug")
	}
}

// Unset must still pin a level explicitly rather than leaving the image's
// baked value to decide -- otherwise the operator's behaviour depends on which
// image tag happens to be deployed.
func TestLoggingLevelDefaultsToInfoInPodEnv(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(3))},
	}
	got, ok := podEnv(t, cluster, "HYPERBYTEDB__LOGGING__LEVEL")
	if !ok {
		t.Fatal("HYPERBYTEDB__LOGGING__LEVEL absent from pod env")
	}
	if got != "info" {
		t.Fatalf("HYPERBYTEDB__LOGGING__LEVEL = %q, want %q", got, "info")
	}
}

// A cluster keeps OrderedReady unless it opts in; a single node is Parallel.
func TestPodManagementIsParallelOnlyWhenAClusterOptsIn(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(3))},
	}
	if got := BuildStatefulSet(cluster, "h").Spec.PodManagementPolicy; got != appsv1.OrderedReadyPodManagement {
		t.Errorf("cluster without the annotation: %s, want OrderedReady", got)
	}
	cluster.Annotations = map[string]string{ParallelPodsAnnotation: "true"}
	if got := BuildStatefulSet(cluster, "h").Spec.PodManagementPolicy; got != appsv1.ParallelPodManagement {
		t.Errorf("cluster with the annotation: %s, want Parallel", got)
	}
	single := &v1alpha1.HyperbytedbCluster{
		Spec: v1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(1))},
	}
	if got := BuildStatefulSet(single, "h").Spec.PodManagementPolicy; got != appsv1.ParallelPodManagement {
		t.Errorf("single node: %s, want Parallel", got)
	}
}

func TestEvacuateTogetherIsOptIn(t *testing.T) {
	cluster := &v1alpha1.HyperbytedbCluster{}
	if EvacuateTogether(cluster) {
		t.Error("on without the annotation")
	}
	cluster.Annotations = map[string]string{EvacuateTogetherAnnotation: "true"}
	if !EvacuateTogether(cluster) {
		t.Error("off with the annotation")
	}
}
