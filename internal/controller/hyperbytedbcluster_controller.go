/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

const clusterFinalizer = "hyperbytedb.hyperbytedb.io/finalizer"

// replicationStateHealthy is the ReplicationState value reported when all
// nodes are within tolerance of each other on parquet file count.
const replicationStateHealthy = "Healthy"

// HyperbytedbClusterReconciler reconciles a HyperbytedbCluster object.
type HyperbytedbClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Members  *hyperbytedb.MemberManager
}

// +kubebuilder:rbac:groups=hyperbytedb.hyperbytedb.io,resources=hyperbytedbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperbytedb.hyperbytedb.io,resources=hyperbytedbclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperbytedb.hyperbytedb.io,resources=hyperbytedbclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

// nolint:gocyclo // Reconcile orchestrates many sequential steps (finalizers, services,
// configmap, scale-down hooks, statefulset, status, replication checks, auto-failover);
// splitting it further would obscure the linear flow without reducing real complexity.
func (r *HyperbytedbClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
	if err := r.Get(ctx, req.NamespacedName, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// ---- Pause check ----
	if cluster.Spec.Paused {
		log.Info("Reconciliation paused")
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Paused",
			Status:             metav1.ConditionTrue,
			Reason:             "UserRequested",
			Message:            "Reconciliation is paused",
			LastTransitionTime: metav1.Now(),
		})
		_ = r.Status().Update(ctx, cluster)
		return ctrl.Result{}, nil
	}
	meta.RemoveStatusCondition(&cluster.Status.Conditions, "Paused")

	// ---- Deletion ----
	if !cluster.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(cluster, clusterFinalizer) {
			r.Recorder.Event(cluster, corev1.EventTypeNormal, "Deleting", "Cleaning up cluster resources")
			controllerutil.RemoveFinalizer(cluster, clusterFinalizer)
			if err := r.Update(ctx, cluster); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// ---- Finalizer ----
	if !controllerutil.ContainsFinalizer(cluster, clusterFinalizer) {
		controllerutil.AddFinalizer(cluster, clusterFinalizer)
		if err := r.Update(ctx, cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}
	configHash := hyperbytedb.ConfigHash(cluster)

	prevSTSReplicas := int32(0)
	curSTS := &appsv1.StatefulSet{}
	switch err := r.Get(ctx, types.NamespacedName{
		Name: hyperbytedb.StatefulSetName(cluster), Namespace: cluster.Namespace,
	}, curSTS); {
	case err == nil && curSTS.Spec.Replicas != nil:
		prevSTSReplicas = *curSTS.Spec.Replicas
	case apierrors.IsNotFound(err):
		break
	default:
		return r.setFailedStatus(ctx, cluster, "StatefulSetLookupFailed", err)
	}

	// 1. TLS Secret (before ConfigMap so the config can reference paths)
	if err := r.reconcileTLS(ctx, cluster, replicas); err != nil {
		return r.setFailedStatus(ctx, cluster, "TLSFailed", err)
	}

	// 2. ConfigMap
	if err := r.reconcileConfigMap(ctx, cluster); err != nil {
		return r.setFailedStatus(ctx, cluster, "ConfigMapFailed", err)
	}

	// 3. Peers ConfigMap
	if err := r.reconcilePeersConfigMap(ctx, cluster, replicas); err != nil {
		return r.setFailedStatus(ctx, cluster, "PeersConfigMapFailed", err)
	}

	// 4. Headless Service
	if err := r.reconcileService(ctx, cluster, hyperbytedb.BuildHeadlessService(cluster)); err != nil {
		return r.setFailedStatus(ctx, cluster, "HeadlessServiceFailed", err)
	}

	// 5. Client Service
	if err := r.reconcileService(ctx, cluster, hyperbytedb.BuildClientService(cluster)); err != nil {
		return r.setFailedStatus(ctx, cluster, "ClientServiceFailed", err)
	}

	// 5b. Scale-down: drain departing pods and notify survivors before the StatefulSet shrinks.
	// Errors per-pod are already logged inside the hook and intentionally non-fatal.
	r.runScaleDownClusterHooks(ctx, cluster, prevSTSReplicas, replicas)

	// 6. StatefulSet
	stsResult, err := r.reconcileStatefulSet(ctx, cluster, configHash)
	if err != nil {
		return r.setFailedStatus(ctx, cluster, "StatefulSetFailed", err)
	}

	// 7. Handle scaling
	if stsResult.SpecReplicas != replicas {
		if replicas > stsResult.SpecReplicas {
			r.Recorder.Eventf(cluster, corev1.EventTypeNormal, "ScalingUp",
				"Scaling from %d to %d replicas", stsResult.SpecReplicas, replicas)
		} else {
			r.Recorder.Eventf(cluster, corev1.EventTypeNormal, "ScalingDown",
				"Scaling from %d to %d replicas", stsResult.SpecReplicas, replicas)
		}
		r.updatePhase(ctx, cluster, hyperbytedbv1alpha1.ClusterPhaseScaling)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// 8. Orphan PVC cleanup after scale-down (pods already terminated by StatefulSet)
	if prevSTSReplicas > replicas {
		if err := hyperbytedb.CleanupOrphanPVCs(ctx, r.Client, cluster, prevSTSReplicas, replicas); err != nil {
			log.Error(err, "Orphan PVC cleanup after scale-down failed")
		}
	}

	// 9. ServiceMonitor (best-effort; Prometheus Operator CRDs may not be installed)
	if cluster.Spec.Monitoring.ServiceMonitor {
		if err := r.reconcileServiceMonitor(ctx, cluster); err != nil {
			log.V(1).Info("ServiceMonitor reconciliation skipped (CRD may not be installed)", "error", err)
		}
	}

	// 10. PDB
	if err := r.reconcilePDB(ctx, cluster); err != nil {
		log.Error(err, "Failed to reconcile PDB")
	}

	// 11. HPA
	if err := r.reconcileHPA(ctx, cluster); err != nil {
		log.Error(err, "Failed to reconcile HPA")
	}

	// 12. Collect member statuses
	memberStatuses := r.Members.CollectMemberStatuses(ctx, cluster, cluster.Namespace, replicas)
	cluster.Status.Members = memberStatuses
	cluster.Status.ClusterState = hyperbytedb.DeriveClusterState(memberStatuses)

	// 13. Cluster topology verification (multi-node only)
	if replicas > 1 {
		r.verifyClusterTopology(ctx, cluster, replicas)
	}

	// 14. Replication health monitoring (multi-node only)
	if replicas > 1 {
		r.monitorReplicationHealth(ctx, cluster, replicas)
	}

	// 15. Auto-failover
	if err := r.handleAutoFailover(ctx, cluster, memberStatuses); err != nil {
		log.Error(err, "Auto-failover handling failed")
	}

	// 16. Update status
	r.updateStatus(ctx, cluster, stsResult, configHash)

	return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// ---------- TLS ----------

func (r *HyperbytedbClusterReconciler) reconcileTLS(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, replicas int32) error {
	if cluster.Spec.Server.TLS == nil || !cluster.Spec.Server.TLS.Enabled {
		return nil
	}

	// If the user provided a secret, trust it
	if cluster.Spec.Server.TLS.SecretName != "" {
		return nil
	}

	// If cert-manager issuer is referenced, skip self-signed generation
	if cluster.Spec.Server.TLS.CertManagerIssuerRef != nil {
		return nil
	}

	secretName := hyperbytedb.TLSSecretName(cluster)
	existing := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: cluster.Namespace}, existing)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	secret, err := hyperbytedb.BuildSelfSignedTLSSecret(cluster, cluster.Namespace, replicas)
	if err != nil {
		return fmt.Errorf("generating TLS secret: %w", err)
	}
	if err := controllerutil.SetControllerReference(cluster, secret, r.Scheme); err != nil {
		return err
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, "TLSCreated", "Generated self-signed TLS certificate")
	return r.Create(ctx, secret)
}

// ---------- ConfigMap ----------

func (r *HyperbytedbClusterReconciler) reconcileConfigMap(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) error {
	desired := hyperbytedb.BuildConfigMap(cluster)
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		existing.Data = desired.Data
		return r.Update(ctx, existing)
	}
	return nil
}

// ---------- Peers ConfigMap ----------

func (r *HyperbytedbClusterReconciler) reconcilePeersConfigMap(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, replicas int32) error {
	desired := hyperbytedb.BuildPeersConfigMap(cluster, cluster.Namespace, replicas)
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	if !equality.Semantic.DeepEqual(existing.Data, desired.Data) {
		existing.Data = desired.Data
		return r.Update(ctx, existing)
	}
	return nil
}

// ---------- Service ----------

func (r *HyperbytedbClusterReconciler) reconcileService(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, desired *corev1.Service) error {
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
	return r.Update(ctx, existing)
}

// ---------- StatefulSet ----------

type stsReconcileResult struct {
	SpecReplicas  int32
	ReadyReplicas int32
}

func (r *HyperbytedbClusterReconciler) reconcileStatefulSet(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, configHash string) (stsReconcileResult, error) {
	desired := hyperbytedb.BuildStatefulSet(cluster, configHash)
	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return stsReconcileResult{}, err
	}

	existing := &appsv1.StatefulSet{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		r.Recorder.Event(cluster, corev1.EventTypeNormal, "CreatingStatefulSet", "Creating StatefulSet for cluster")
		if err := r.Create(ctx, desired); err != nil {
			return stsReconcileResult{}, err
		}
		return stsReconcileResult{SpecReplicas: *desired.Spec.Replicas}, nil
	}
	if err != nil {
		return stsReconcileResult{}, err
	}

	// Update mutable fields
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Template.Spec.Containers = desired.Spec.Template.Spec.Containers
	existing.Spec.Template.Spec.InitContainers = desired.Spec.Template.Spec.InitContainers
	existing.Spec.Template.Spec.Volumes = desired.Spec.Template.Spec.Volumes
	existing.Spec.Template.Spec.ImagePullSecrets = desired.Spec.Template.Spec.ImagePullSecrets
	existing.Spec.Template.Spec.Affinity = desired.Spec.Template.Spec.Affinity
	existing.Spec.Template.Spec.Tolerations = desired.Spec.Template.Spec.Tolerations
	existing.Spec.Template.Spec.TopologySpreadConstraints = desired.Spec.Template.Spec.TopologySpreadConstraints
	existing.Spec.Template.Annotations = desired.Spec.Template.Annotations
	existing.Spec.Template.Labels = desired.Spec.Template.Labels
	existing.Spec.UpdateStrategy = desired.Spec.UpdateStrategy

	if err := r.Update(ctx, existing); err != nil {
		return stsReconcileResult{}, err
	}

	return stsReconcileResult{
		SpecReplicas:  *existing.Spec.Replicas,
		ReadyReplicas: existing.Status.ReadyReplicas,
	}, nil
}

// ---------- Scale-down handling ----------

// runScaleDownClusterHooks drains pods that will be removed (highest ordinals first) and asks
// surviving members to drop them from membership. Must run before .spec.replicas is reduced.
// Per-pod failures are logged and ignored: the StatefulSet scale-down still proceeds because
// the membership/drain endpoints are best-effort hints to surviving nodes.
func (r *HyperbytedbClusterReconciler) runScaleDownClusterHooks(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, prevReplicas, desiredReplicas int32) {
	if desiredReplicas >= prevReplicas || prevReplicas < 1 {
		return
	}

	log := logf.FromContext(ctx)
	port := hyperbytedb.ServerPort(cluster)
	stsName := hyperbytedb.StatefulSetName(cluster)
	headlessSvc := hyperbytedb.HeadlessServiceName(cluster)

	for i := prevReplicas - 1; i >= desiredReplicas; i-- {
		host := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local",
			stsName, i, headlessSvc, cluster.Namespace)
		if err := r.Members.Client.DrainNode(ctx, host, port); err != nil {
			log.V(1).Info("Could not drain departing pod", "ordinal", i, "host", host, "error", err)
		}
	}

	for i := desiredReplicas; i < prevReplicas; i++ {
		departedID := uint64(i + 1)
		for j := range desiredReplicas {
			survivor := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local",
				stsName, j, headlessSvc, cluster.Namespace)
			if err := r.Members.Client.LeaveNode(ctx, survivor, port, departedID); err != nil {
				log.V(1).Info("Could not notify survivor of departed node",
					"survivor", survivor, "departedNodeID", departedID, "error", err)
			}
		}
	}
}

// ---------- Auto-failover ----------

func (r *HyperbytedbClusterReconciler) handleAutoFailover(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, members []hyperbytedbv1alpha1.MemberStatus) error {
	if cluster.Spec.Failover == nil || !cluster.Spec.Failover.Enabled {
		return nil
	}

	maxFailovers := cluster.Spec.Failover.MaxFailoverCount
	if maxFailovers <= 0 {
		maxFailovers = 1
	}
	if cluster.Status.FailoverCount >= maxFailovers {
		return nil
	}

	timeoutSecs := cluster.Spec.Failover.FailoverTimeoutSecs
	if timeoutSecs <= 0 {
		timeoutSecs = 300
	}
	timeout := time.Duration(timeoutSecs) * time.Second

	log := logf.FromContext(ctx)

	for _, m := range hyperbytedb.FindUnhealthyMembers(members) {
		if m.State == "Syncing" || m.State == "Joining" {
			continue // give syncing nodes time
		}

		age := time.Since(m.LastTransitionTime.Time)
		if age < timeout {
			continue
		}

		log.Info("Triggering auto-failover for member", "pod", m.PodName, "state", m.State, "unhealthyFor", age.String())

		pod := &corev1.Pod{}
		err := r.Get(ctx, types.NamespacedName{Name: m.PodName, Namespace: cluster.Namespace}, pod)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}

		if err := r.Delete(ctx, pod); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting failed pod %s: %w", m.PodName, err)
		}

		r.Recorder.Eventf(cluster, corev1.EventTypeWarning, "AutoFailover",
			"Deleted unhealthy pod %s (state=%s, unhealthy for %s)", m.PodName, m.State, age.Truncate(time.Second))

		cluster.Status.FailoverCount++
		break // one failover per reconcile cycle
	}

	return nil
}

// ---------- ServiceMonitor ----------

func (r *HyperbytedbClusterReconciler) reconcileServiceMonitor(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) error {
	desired := hyperbytedb.BuildServiceMonitor(cluster)

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := desired.DeepCopy()
	err := r.Get(ctx, types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	return err
}

// ---------- PDB ----------

func (r *HyperbytedbClusterReconciler) reconcilePDB(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) error {
	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}

	pdbName := cluster.Name + "-pdb"

	if replicas < 3 {
		existing := &policyv1.PodDisruptionBudget{}
		err := r.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: cluster.Namespace}, existing)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return r.Delete(ctx, existing)
	}

	minAvail := intstr.FromInt32(replicas - 1)
	desired := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdbName,
			Namespace: cluster.Namespace,
			Labels:    hyperbytedb.CommonLabels(cluster),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvail,
			Selector: &metav1.LabelSelector{
				MatchLabels: hyperbytedb.CommonLabels(cluster),
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &policyv1.PodDisruptionBudget{}
	err := r.Get(ctx, types.NamespacedName{Name: pdbName, Namespace: cluster.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Spec = desired.Spec
	return r.Update(ctx, existing)
}

// ---------- HPA ----------

func (r *HyperbytedbClusterReconciler) reconcileHPA(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) error {
	hpaName := cluster.Name

	if cluster.Spec.Autoscaling == nil || !cluster.Spec.Autoscaling.Enabled {
		existing := &autoscalingv2.HorizontalPodAutoscaler{}
		err := r.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: cluster.Namespace}, existing)
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return r.Delete(ctx, existing)
	}

	as := cluster.Spec.Autoscaling
	targetCPU := as.TargetCPUUtilizationPercentage
	if targetCPU == 0 {
		targetCPU = 80
	}

	desired := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      hpaName,
			Namespace: cluster.Namespace,
			Labels:    hyperbytedb.CommonLabels(cluster),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       hyperbytedb.StatefulSetName(cluster),
			},
			MinReplicas: &as.MinReplicas,
			MaxReplicas: as.MaxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &targetCPU,
						},
					},
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}

	existing := &autoscalingv2.HorizontalPodAutoscaler{}
	err := r.Get(ctx, types.NamespacedName{Name: hpaName, Namespace: cluster.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Spec = desired.Spec
	return r.Update(ctx, existing)
}

// ---------- Cluster Topology Verification ----------

func (r *HyperbytedbClusterReconciler) verifyClusterTopology(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, replicas int32) {
	log := logf.FromContext(ctx)
	port := hyperbytedb.ServerPort(cluster)
	stsName := hyperbytedb.StatefulSetName(cluster)
	headlessSvc := hyperbytedb.HeadlessServiceName(cluster)

	type podView struct {
		ordinal  int32
		host     string
		nodeID   int32
		nodeAddr string
		nodes    []hyperbytedb.NodeInfo
	}

	var views []podView
	for i := range replicas {
		host := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local",
			stsName, i, headlessSvc, cluster.Namespace)
		nodes, err := r.Members.Client.GetClusterNodes(ctx, host, port)
		if err != nil {
			log.V(1).Info("Could not query cluster/nodes", "ordinal", i, "error", err)
			continue
		}
		views = append(views, podView{
			ordinal:  i,
			host:     host,
			nodeID:   i + 1,
			nodeAddr: fmt.Sprintf("%s:%d", host, port),
			nodes:    nodes,
		})
	}

	if len(views) == 0 {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "ClusterFormed",
			Status:             metav1.ConditionFalse,
			Reason:             "NoPodReachable",
			Message:            "Could not query any pod for cluster topology",
			LastTransitionTime: metav1.Now(),
		})
		return
	}

	// Build map of known nodeIDs per view and detect missing nodes
	allExpected := make(map[int32]string, replicas)
	for i := range replicas {
		allExpected[i+1] = fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local:%d",
			stsName, i, headlessSvc, cluster.Namespace, port)
	}

	// Find the leader candidate: the node with the most peers (most complete view).
	// Sort views descending by peer count so we try the likely leader first.
	leaderCandidates := make([]podView, len(views))
	copy(leaderCandidates, views)
	sort.Slice(leaderCandidates, func(i, j int) bool {
		return len(leaderCandidates[i].nodes) > len(leaderCandidates[j].nodes)
	})

	// Collect all globally missing memberships (node missing from ANY view).
	type missingEntry struct {
		nodeID int32
		addr   string
	}
	seen := make(map[int32]bool)
	var missing []missingEntry
	fullyFormed := true
	for _, v := range views {
		knownIDs := make(map[int]bool, len(v.nodes))
		for _, n := range v.nodes {
			knownIDs[n.NodeID] = true
		}
		for expectedID, expectedAddr := range allExpected {
			if knownIDs[int(expectedID)] {
				continue
			}
			fullyFormed = false
			if !seen[expectedID] {
				seen[expectedID] = true
				missing = append(missing, missingEntry{nodeID: expectedID, addr: expectedAddr})
			}
		}
	}

	// Send join requests to the leader candidate for each missing node.
	// If the first candidate isn't the leader, try the next one.
	for _, m := range missing {
		joined := false
		for _, candidate := range leaderCandidates {
			log.Info("Sending join request to leader candidate",
				"leaderOrdinal", candidate.ordinal, "missingNodeID", m.nodeID)
			if err := r.Members.Client.JoinNode(ctx, candidate.host, port, int(m.nodeID), m.addr); err != nil {
				log.V(1).Info("Join request failed, trying next candidate",
					"target", candidate.host, "missingNodeID", m.nodeID, "error", err)
				continue
			}
			joined = true
			log.Info("Successfully joined node via leader",
				"leaderOrdinal", candidate.ordinal, "missingNodeID", m.nodeID)
			break
		}
		if !joined {
			log.Info("Could not join node via any candidate", "missingNodeID", m.nodeID)
		}
	}

	// Update per-member PeerCount
	for _, v := range views {
		for idx := range cluster.Status.Members {
			if cluster.Status.Members[idx].NodeID == v.nodeID {
				cluster.Status.Members[idx].PeerCount = int32(len(v.nodes)) - 1
				break
			}
		}
	}

	if fullyFormed {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "ClusterFormed",
			Status:             metav1.ConditionTrue,
			Reason:             "AllNodesVisible",
			Message:            fmt.Sprintf("All %d nodes see the full cluster topology", len(views)),
			LastTransitionTime: metav1.Now(),
		})
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "ClusterFormed",
			Status:             metav1.ConditionFalse,
			Reason:             "TopologyIncomplete",
			Message:            "Not all nodes have a complete view of the cluster",
			LastTransitionTime: metav1.Now(),
		})
	}
}

// ---------- Replication Health Monitoring ----------

func (r *HyperbytedbClusterReconciler) monitorReplicationHealth(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, replicas int32) {
	log := logf.FromContext(ctx)
	port := hyperbytedb.ServerPort(cluster)
	stsName := hyperbytedb.StatefulSetName(cluster)
	headlessSvc := hyperbytedb.HeadlessServiceName(cluster)

	type nodeManifest struct {
		ordinal      int32
		walSeq       int64
		parquetFiles int32
	}

	var manifests []nodeManifest
	for i := range replicas {
		host := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local",
			stsName, i, headlessSvc, cluster.Namespace)
		manifest, err := r.Members.Client.GetSyncManifest(ctx, host, port)
		if err != nil {
			log.V(1).Info("Could not query sync/manifest", "ordinal", i, "error", err)
			continue
		}

		nm := nodeManifest{
			ordinal:      i,
			walSeq:       manifest.WALLastSeq,
			parquetFiles: int32(manifest.TotalParquetFiles()),
		}
		manifests = append(manifests, nm)

		// Populate per-member status
		for idx := range cluster.Status.Members {
			if cluster.Status.Members[idx].NodeID == i+1 {
				cluster.Status.Members[idx].WALSequence = manifest.WALLastSeq
				cluster.Status.Members[idx].ParquetFiles = int32(manifest.TotalParquetFiles())
				break
			}
		}
	}

	if len(manifests) < 2 {
		cluster.Status.ReplicationState = "Unknown"
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "ReplicationHealthy",
			Status:             metav1.ConditionUnknown,
			Reason:             "InsufficientData",
			Message:            "Could not query enough nodes for replication comparison",
			LastTransitionTime: metav1.Now(),
		})
		return
	}

	var maxFiles, minFiles int32
	maxFiles = manifests[0].parquetFiles
	minFiles = manifests[0].parquetFiles
	for _, m := range manifests[1:] {
		if m.parquetFiles > maxFiles {
			maxFiles = m.parquetFiles
		}
		if m.parquetFiles < minFiles {
			minFiles = m.parquetFiles
		}
	}

	replState := replicationStateHealthy
	reason := "InSync"
	if maxFiles > 0 && minFiles < maxFiles/2 {
		replState = "Diverged"
		reason = "LargeDataGap"
	} else if maxFiles > 0 && minFiles < maxFiles*8/10 {
		replState = "Lagging"
		reason = "DataLag"
	}

	cluster.Status.ReplicationState = replState

	condStatus := metav1.ConditionTrue
	if replState != replicationStateHealthy {
		condStatus = metav1.ConditionFalse
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               "ReplicationHealthy",
		Status:             condStatus,
		Reason:             reason,
		Message:            fmt.Sprintf("Parquet files: min=%d max=%d across %d nodes", minFiles, maxFiles, len(manifests)),
		LastTransitionTime: metav1.Now(),
	})

	if replState != replicationStateHealthy {
		log.Info("Replication divergence detected",
			"state", replState, "minFiles", minFiles, "maxFiles", maxFiles)
	}
}

// ---------- Status ----------

func (r *HyperbytedbClusterReconciler) updateStatus(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, sts stsReconcileResult, configHash string) {
	cluster.Status.Replicas = sts.SpecReplicas
	cluster.Status.ReadyReplicas = sts.ReadyReplicas
	cluster.Status.ConfigHash = configHash

	if sts.ReadyReplicas == sts.SpecReplicas && sts.SpecReplicas > 0 {
		cluster.Status.Phase = hyperbytedbv1alpha1.ClusterPhaseRunning
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionTrue,
			Reason:             "AllReplicasReady",
			Message:            fmt.Sprintf("%d/%d replicas ready", sts.ReadyReplicas, sts.SpecReplicas),
			LastTransitionTime: metav1.Now(),
		})
	} else {
		if cluster.Status.Phase != hyperbytedbv1alpha1.ClusterPhaseFailed &&
			cluster.Status.Phase != hyperbytedbv1alpha1.ClusterPhaseScaling &&
			cluster.Status.Phase != hyperbytedbv1alpha1.ClusterPhaseUpgrading {
			cluster.Status.Phase = hyperbytedbv1alpha1.ClusterPhaseInitializing
		}
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               "Available",
			Status:             metav1.ConditionFalse,
			Reason:             "ReplicasNotReady",
			Message:            fmt.Sprintf("%d/%d replicas ready", sts.ReadyReplicas, sts.SpecReplicas),
			LastTransitionTime: metav1.Now(),
		})
	}

	_ = r.Status().Update(ctx, cluster)
}

func (r *HyperbytedbClusterReconciler) updatePhase(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, phase hyperbytedbv1alpha1.ClusterPhase) {
	cluster.Status.Phase = phase
	_ = r.Status().Update(ctx, cluster)
}

func (r *HyperbytedbClusterReconciler) setFailedStatus(ctx context.Context, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, reason string, err error) (ctrl.Result, error) {
	cluster.Status.Phase = hyperbytedbv1alpha1.ClusterPhaseFailed
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               "Degraded",
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            err.Error(),
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, cluster)
	r.Recorder.Eventf(cluster, corev1.EventTypeWarning, reason, "Reconciliation failed: %v", err)
	return ctrl.Result{RequeueAfter: 30 * time.Second}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *HyperbytedbClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hyperbytedbv1alpha1.HyperbytedbCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Named("hyperbytedbcluster").
		Complete(r)
}
