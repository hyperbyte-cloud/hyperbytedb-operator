package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// The CRD defaulted both Raft timings to 1000, which Raft refuses: a UAT
// cluster created without them crash-looped on every node.
var _ = Describe("Raft timing defaults", func() {
	cr := func(name string) *hyperbytedbv1alpha1.HyperbytedbCluster {
		return &hyperbytedbv1alpha1.HyperbytedbCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       hyperbytedbv1alpha1.HyperbytedbClusterSpec{Replicas: ptr.To(int32(3))},
		}
	}

	It("defaults the heartbeat below the election timeout", func() {
		c := cr("raft-defaults")
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		got := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: c.Name, Namespace: "default"}, got)).To(Succeed())
		Expect(got.Spec.Cluster.RaftHeartbeatIntervalMs).To(BeNumerically("<", got.Spec.Cluster.RaftElectionTimeoutMs))
	})

	It("refuses a heartbeat at or above the election timeout", func() {
		c := cr("raft-equal")
		c.Spec.Cluster.RaftHeartbeatIntervalMs = 1000
		c.Spec.Cluster.RaftElectionTimeoutMs = 1000
		err := k8sClient.Create(ctx, c)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("raftHeartbeatIntervalMs must be below raftElectionTimeoutMs"))
	})
})
