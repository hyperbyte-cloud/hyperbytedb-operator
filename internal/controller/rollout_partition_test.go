package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

// A one-node pool restarted its pod twice in one rolling update: with
// partition 0 the StatefulSet controller replaced the pod as soon as the
// template changed, then the operator's own drain step deleted the
// replacement. Envtest runs no StatefulSet controller, so playStatefulSet
// below does its part: replace, highest first, the pods the partition
// releases.
var _ = Describe("Rolling update through the StatefulSet partition", func() {
	ctx := context.Background()
	var name string
	var key types.NamespacedName
	pools := 0

	reconcileOnce := func() {
		r := &HyperbytedbClusterReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(256),
			Members:  hyperbytedb.NewMemberManager(),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
	}

	getSTS := func() *appsv1.StatefulSet {
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		return sts
	}
	partition := func() int32 {
		ru := getSTS().Spec.UpdateStrategy.RollingUpdate
		Expect(ru).NotTo(BeNil())
		Expect(ru.Partition).NotTo(BeNil())
		return *ru.Partition
	}
	getState := func() *hyperbytedbv1alpha1.RollingRestartState {
		c := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, c)).To(Succeed())
		return c.Status.RollingRestart
	}

	podName := func(ordinal int) string { return fmt.Sprintf("%s-%d", name, ordinal) }
	createPod := func(ordinal int, revision string) {
		pod := testPod(podName(ordinal), map[string]string{
			"app.kubernetes.io/name":        "hyperbytedb",
			appsv1.StatefulSetRevisionLabel: revision,
		}, fmt.Sprintf("127.0.0.%d", ordinal+2), "hyperbytedb", true)
		Expect(createWithStatus(ctx, pod)).To(Succeed())
	}
	podRevision := func(ordinal int) string {
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podName(ordinal), Namespace: "default"}, pod)).To(Succeed())
		return pod.Labels[appsv1.StatefulSetRevisionLabel]
	}

	// setStatus writes what the StatefulSet controller would report for
	// these pod revisions, completing the update once every pod has it.
	setStatus := func(replicas int, current, update string) {
		updated := int32(0)
		for i := range replicas {
			if podRevision(i) == update {
				updated++
			}
		}
		if updated == int32(replicas) {
			current = update
		}
		sts := getSTS()
		sts.Status.Replicas = int32(replicas)
		sts.Status.ReadyReplicas = int32(replicas)
		sts.Status.UpdatedReplicas = updated
		sts.Status.CurrentRevision = current
		sts.Status.UpdateRevision = update
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())
	}

	// playStatefulSet replaces the highest released pod not yet on the
	// update revision, as the StatefulSet controller does: one at a time.
	playStatefulSet := func(replicas int, update string) (int, bool) {
		p := int(partition())
		for i := replicas - 1; i >= p; i-- {
			if podRevision(i) == update {
				continue
			}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName(i), Namespace: "default"}}
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
			createPod(i, update)
			return i, true
		}
		return 0, false
	}

	// backdate makes a drain wait already over.
	backdate := func() {
		c := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, c)).To(Succeed())
		if c.Status.RollingRestart != nil &&
			c.Status.RollingRestart.Phase == hyperbytedbv1alpha1.RollingRestartDraining {
			c.Status.RollingRestart.PhaseStartedAt = metav1.NewTime(time.Now().Add(-time.Hour))
			Expect(k8sClient.Status().Update(ctx, c)).To(Succeed())
		}
	}

	newPool := func(replicas int) {
		pools++
		name = fmt.Sprintf("rollout-pool-%d", pools)
		key = types.NamespacedName{Name: name, Namespace: "default"}
		Expect(k8sClient.Create(ctx, &hyperbytedbv1alpha1.HyperbytedbCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
				Replicas: ptr.To(int32(replicas)),
				Version:  "0.9.1-beta",
				Proxy:    &hyperbytedbv1alpha1.ProxySpec{Enabled: true},
			},
		})).To(Succeed())
		reconcileOnce()
	}

	AfterEach(func() {
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace("default"))
	})

	// rollOut changes the template and drives the update to the end. It
	// returns the ordinals in the order they were replaced, and the restart
	// phase the operator was in for each.
	rollOut := func(replicas int) ([]int, []hyperbytedbv1alpha1.RollingRestartPhase) {
		c := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, c)).To(Succeed())
		c.Spec.Version = "0.9.2-beta"
		Expect(k8sClient.Update(ctx, c)).To(Succeed())
		setStatus(replicas, "old", "new")

		var order []int
		var phases []hyperbytedbv1alpha1.RollingRestartPhase
		for range 20 * replicas {
			reconcileOnce()
			if i, ok := playStatefulSet(replicas, "new"); ok {
				order = append(order, i)
				phases = append(phases, getState().Phase)
			}
			setStatus(replicas, "old", "new")
			backdate()
			if getState() == nil && podRevision(0) == "new" {
				break
			}
		}
		return order, phases
	}

	for _, replicas := range []int{1, 3} {
		It(fmt.Sprintf("replaces each of %d pods once, highest first, after its drain", replicas), func() {
			newPool(replicas)
			Expect(partition()).To(Equal(int32(replicas)), "pinned before any update")
			for i := range replicas {
				createPod(i, "old")
			}
			setStatus(replicas, "old", "old")

			order, phases := rollOut(replicas)

			want := make([]int, 0, replicas)
			for i := replicas - 1; i >= 0; i-- {
				want = append(want, i)
			}
			Expect(order).To(Equal(want), "each pod replaced once, highest ordinal first")
			for _, ph := range phases {
				Expect(ph).To(Equal(hyperbytedbv1alpha1.RollingRestartWaitingReady),
					"a pod is replaced only once the operator has excluded and drained it")
			}
			Expect(getState()).To(BeNil(), "the restart completes")
			reconcileOnce()
			Expect(partition()).To(Equal(int32(replicas)), "pinned again for the next update")
		})
	}

	It("leaves a pod that already runs the update revision alone", func() {
		newPool(2)
		createPod(0, "old")
		createPod(1, "new")
		setStatus(2, "old", "new")

		order, _ := rollOut(2)
		Expect(order).To(Equal([]int{0}))
		Expect(getState()).To(BeNil())
	})
})
