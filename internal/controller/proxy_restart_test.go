package controller

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

// A pool whose proxy was down when a rolling restart began: the restart
// retried excluding pod 0 from the proxies forever, and the proxy
// Deployment, applied after the restart step, was never fixed.
var _ = Describe("Rolling restart with the proxy down", func() {
	ctx := context.Background()
	// A fresh pool per test: the reconciler adds a finalizer that nothing
	// removes here, so a deleted pool lingers.
	var name string
	var key types.NamespacedName
	pools := 0

	reconciler := func() *HyperbytedbClusterReconciler {
		return &HyperbytedbClusterReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(64),
			Members:  hyperbytedb.NewMemberManager(),
		}
	}

	BeforeEach(func() {
		pools++
		name = fmt.Sprintf("stuck-pool-%d", pools)
		key = types.NamespacedName{Name: name, Namespace: "default"}
		Expect(k8sClient.Create(ctx, &hyperbytedbv1alpha1.HyperbytedbCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
				Replicas: ptr.To(int32(1)),
				Version:  "0.9.1-beta",
				Proxy:    &hyperbytedbv1alpha1.ProxySpec{Enabled: true},
			},
		})).To(Succeed())
		// First pass creates the StatefulSet.
		_, _ = reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
	})

	AfterEach(func() {
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace("default"))
	})

	// Seed the stuck state: a restart waiting to exclude pod 0, an update
	// pending on the StatefulSet, and the only proxy pod crash-looping.
	seedStuckRestart := func() {
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		sts.Status.Replicas = 1
		sts.Status.CurrentRevision = "old"
		sts.Status.UpdateRevision = "new"
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())

		cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		cluster.Status.RollingRestart = &hyperbytedbv1alpha1.RollingRestartState{
			TotalOrdinals:  1,
			Phase:          hyperbytedbv1alpha1.RollingRestartExcluding,
			PhaseStartedAt: metav1.Now(),
		}
		Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

		db := testPod(name+"-0", map[string]string{"app.kubernetes.io/name": "hyperbytedb"}, "10.0.0.10", "hyperbytedb", true)
		Expect(createWithStatus(ctx, db)).To(Succeed())
		proxy := testPod(name+"-proxy-x", proxyPodLabels(name), "10.0.0.20", "proxy", false)
		Expect(createWithStatus(ctx, proxy)).To(Succeed())
	}

	It("applies the proxy Deployment and gets past the exclusion", func() {
		seedStuckRestart()
		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())

		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name + "-proxy", Namespace: "default"}, dep)).
			To(Succeed(), "the proxy Deployment is applied before the restart step")
		probe := dep.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Port.String()
		Expect(probe).To(Equal("admin"))

		cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		Expect(cluster.Status.RollingRestart).NotTo(BeNil())
		Expect(cluster.Status.RollingRestart.ExcludeConfirmed).To(BeTrue(),
			"no proxy is running, so there is nothing to exclude from and the restart moves on")
	})

	It("clears a restart that never started once no update is pending", func() {
		seedStuckRestart()
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, key, sts)).To(Succeed())
		sts.Status.UpdateRevision = sts.Status.CurrentRevision
		Expect(k8sClient.Status().Update(ctx, sts)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		Expect(cluster.Status.RollingRestart).To(BeNil())
	})

	It("clears a stale Degraded condition once a reconcile completes", func() {
		cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type: "Degraded", Status: metav1.ConditionTrue,
			Reason: "RollingRestartFailed", Message: "from an earlier failure",
		})
		Expect(k8sClient.Status().Update(ctx, cluster)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: key})
		Expect(err).NotTo(HaveOccurred())
		Expect(k8sClient.Get(ctx, key, cluster)).To(Succeed())
		cond := meta.FindStatusCondition(cluster.Status.Conditions, "Degraded")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("Reconciled"))
	})
})

// One proxy pod whose process runs but is not Ready (its only backend is
// excluded) and one that is crash-looping: exclude and include go to the
// running one only, and are not held for readiness. Requiring Ready made the
// include that would make a one-node pool's proxies Ready again never go out.
var _ = Describe("Proxy exclude and include", func() {
	ctx := context.Background()
	const name = "one-node-pool"

	It("reach every running proxy, Ready or not, and skip the rest", func() {
		var mu sync.Mutex
		var got []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			got = append(got, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
		port, _ := strconv.Atoi(portStr)

		running := testPod(name+"-proxy-a", proxyPodLabels(name), host, "proxy", false)
		running.Status.ContainerStatuses[0].State = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
		Expect(createWithStatus(ctx, running)).To(Succeed())
		down := testPod(name+"-proxy-b", proxyPodLabels(name), "10.0.0.31", "proxy", false)
		Expect(createWithStatus(ctx, down)).To(Succeed())
		defer func() { _ = k8sClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace("default")) }()

		r := &HyperbytedbClusterReconciler{Client: k8sClient, Members: hyperbytedb.NewMemberManager()}
		cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"}}
		Expect(r.excludeFromAllProxies(ctx, cluster, "10.0.0.10", int32(port))).To(Succeed())
		Expect(r.includeFromAllProxies(ctx, cluster, "10.0.0.10", int32(port))).To(Succeed())
		Expect(got).To(Equal([]string{
			"/admin/backends/10.0.0.10/exclude",
			"/admin/backends/10.0.0.10/include",
		}))
	})
})

func proxyPodLabels(cluster string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "hyperbytedb-proxy",
		"app.kubernetes.io/instance":   cluster,
		"app.kubernetes.io/managed-by": "hyperbytedb-operator",
		"app.kubernetes.io/component":  "proxy",
	}
}

// testPod is a pod with one container: Ready and running when ready is
// true, else crash-looping (waiting, not Ready).
func testPod(name string, labels map[string]string, ip, container string, ready bool) *corev1.Pod {
	state := corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}
	readyStatus := corev1.ConditionFalse
	if ready {
		state = corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
		readyStatus = corev1.ConditionTrue
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: labels},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: container, Image: "x"}}},
		Status: corev1.PodStatus{
			PodIP:             ip,
			Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: readyStatus}},
			ContainerStatuses: []corev1.ContainerStatus{{Name: container, Ready: ready, State: state}},
		},
	}
}

// createWithStatus creates the pod, then writes its status (create ignores it).
func createWithStatus(ctx context.Context, pod *corev1.Pod) error {
	status := pod.Status
	if err := k8sClient.Create(ctx, pod); err != nil {
		return fmt.Errorf("create %s: %w", pod.Name, err)
	}
	pod.Status = status
	return k8sClient.Status().Update(ctx, pod)
}
