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
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

// HyperbytedbRestoreReconciler reconciles a HyperbytedbRestore object.
type HyperbytedbRestoreReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbrestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbrestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbrestores/finalizers,verbs=update
// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbbackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete

func (r *HyperbytedbRestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	restore := &hyperbytedbv1alpha1.HyperbytedbRestore{}
	if err := r.Get(ctx, req.NamespacedName, restore); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if restore.Status.Phase == hyperbytedbv1alpha1.RestorePhaseCompleted ||
		restore.Status.Phase == hyperbytedbv1alpha1.RestorePhaseFailed {
		return ctrl.Result{}, nil
	}

	cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      restore.Spec.ClusterName,
		Namespace: restore.Namespace,
	}, cluster); err != nil {
		log.Error(err, "Referenced cluster not found")
		r.setRestorePhase(ctx, restore, hyperbytedbv1alpha1.RestorePhaseFailed, "ClusterNotFound", err.Error())
		return ctrl.Result{}, nil
	}

	// Resolve S3 source from the backup CR or from the explicit source
	s3Source, err := r.resolveS3Source(ctx, restore)
	if err != nil {
		r.setRestorePhase(ctx, restore, hyperbytedbv1alpha1.RestorePhaseFailed, "SourceResolutionFailed", err.Error())
		return ctrl.Result{}, nil
	}

	switch restore.Status.Phase {
	case "", hyperbytedbv1alpha1.RestorePhasePending:
		return r.handleScaleDown(ctx, restore, cluster)
	case hyperbytedbv1alpha1.RestorePhaseScaleDown:
		return r.checkScaleDown(ctx, restore, cluster, s3Source)
	case hyperbytedbv1alpha1.RestorePhaseRestoring:
		return r.checkRestoreJob(ctx, restore, cluster)
	}

	return ctrl.Result{}, nil
}

func (r *HyperbytedbRestoreReconciler) resolveS3Source(ctx context.Context, restore *hyperbytedbv1alpha1.HyperbytedbRestore) (*hyperbytedbv1alpha1.S3BackupSpec, error) {
	if restore.Spec.Source != nil {
		return &restore.Spec.Source.S3, nil
	}

	backup := &hyperbytedbv1alpha1.HyperbytedbBackup{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      restore.Spec.BackupName,
		Namespace: restore.Namespace,
	}, backup); err != nil {
		return nil, fmt.Errorf("looking up backup %s: %w", restore.Spec.BackupName, err)
	}

	return &hyperbytedbv1alpha1.S3BackupSpec{
		Bucket:                backup.Spec.Destination.S3.Bucket,
		Prefix:                backup.Spec.Destination.S3.Prefix,
		Region:                backup.Spec.Destination.S3.Region,
		Endpoint:              backup.Spec.Destination.S3.Endpoint,
		CredentialsSecretName: backup.Spec.Destination.S3.CredentialsSecretName,
	}, nil
}

func (r *HyperbytedbRestoreReconciler) handleScaleDown(ctx context.Context, restore *hyperbytedbv1alpha1.HyperbytedbRestore, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) (ctrl.Result, error) {
	stsName := hyperbytedb.StatefulSetName(cluster)
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: stsName, Namespace: cluster.Namespace}, sts); err != nil {
		return ctrl.Result{}, err
	}

	zero := int32(0)
	sts.Spec.Replicas = &zero
	if err := r.Update(ctx, sts); err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	restore.Status.StartTime = &now
	r.Recorder.Event(restore, corev1.EventTypeNormal, "ScalingDown", "Scaling cluster to 0 for restore")
	r.setRestorePhase(ctx, restore, hyperbytedbv1alpha1.RestorePhaseScaleDown, "ScalingDown", "Scaling StatefulSet to 0")
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *HyperbytedbRestoreReconciler) checkScaleDown(ctx context.Context, restore *hyperbytedbv1alpha1.HyperbytedbRestore, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, s3Source *hyperbytedbv1alpha1.S3BackupSpec) (ctrl.Result, error) {
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: hyperbytedb.StatefulSetName(cluster), Namespace: cluster.Namespace}, sts); err != nil {
		return ctrl.Result{}, err
	}

	if sts.Status.ReadyReplicas > 0 {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}

	// Create restore jobs for all PVCs (not just pod-0)
	for i := int32(0); i < replicas; i++ {
		job := r.buildRestoreJob(restore, cluster, s3Source, i)
		if err := controllerutil.SetControllerReference(restore, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, job); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
	}

	restore.Status.RestoredPVCs = replicas
	r.Recorder.Eventf(restore, corev1.EventTypeNormal, "RestoreStarted",
		"Launched %d restore jobs", replicas)
	r.setRestorePhase(ctx, restore, hyperbytedbv1alpha1.RestorePhaseRestoring, "RestoreJobsCreated",
		fmt.Sprintf("Created %d restore jobs", replicas))
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *HyperbytedbRestoreReconciler) checkRestoreJob(ctx context.Context, restore *hyperbytedbv1alpha1.HyperbytedbRestore, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) (ctrl.Result, error) {
	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}

	allDone := true
	anyFailed := false

	for i := int32(0); i < replicas; i++ {
		jobName := fmt.Sprintf("%s-restore-%d", restore.Name, i)
		job := &batchv1.Job{}
		if err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: restore.Namespace}, job); err != nil {
			if apierrors.IsNotFound(err) {
				allDone = false
				continue
			}
			return ctrl.Result{}, err
		}

		if job.Status.Succeeded == 0 && job.Status.Failed == 0 {
			allDone = false
		}
		if job.Status.Failed > 0 {
			anyFailed = true
		}
	}

	if anyFailed {
		r.Recorder.Event(restore, corev1.EventTypeWarning, "RestoreFailed", "One or more restore jobs failed")
		r.setRestorePhase(ctx, restore, hyperbytedbv1alpha1.RestorePhaseFailed, "RestoreFailed", "Restore job failed")
		return ctrl.Result{}, nil
	}

	if allDone {
		return r.handleScaleUp(ctx, restore, cluster)
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *HyperbytedbRestoreReconciler) handleScaleUp(ctx context.Context, restore *hyperbytedbv1alpha1.HyperbytedbRestore, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) (ctrl.Result, error) {
	sts := &appsv1.StatefulSet{}
	if err := r.Get(ctx, types.NamespacedName{Name: hyperbytedb.StatefulSetName(cluster), Namespace: cluster.Namespace}, sts); err != nil {
		return ctrl.Result{}, err
	}

	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}
	sts.Spec.Replicas = &replicas
	if err := r.Update(ctx, sts); err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	restore.Status.CompletionTime = &now
	r.Recorder.Event(restore, corev1.EventTypeNormal, "RestoreCompleted", "Restore completed, cluster scaling back up")
	r.setRestorePhase(ctx, restore, hyperbytedbv1alpha1.RestorePhaseCompleted, "RestoreComplete", "Restore completed, cluster scaling back up")
	return ctrl.Result{}, nil
}

func (r *HyperbytedbRestoreReconciler) buildRestoreJob(
	restore *hyperbytedbv1alpha1.HyperbytedbRestore,
	cluster *hyperbytedbv1alpha1.HyperbytedbCluster,
	s3Source *hyperbytedbv1alpha1.S3BackupSpec,
	ordinal int32,
) *batchv1.Job {
	image := hyperbytedb.ResolveHyperbytedbImage(cluster)

	s3Path := s3Source.Bucket
	if s3Source.Prefix != "" {
		s3Path += "/" + strings.TrimSuffix(s3Source.Prefix, "/")
	}

	syncCmd := fmt.Sprintf(`aws s3 sync "s3://%s/" /tmp/backup/`, s3Path)
	if s3Source.Endpoint != "" {
		syncCmd = fmt.Sprintf(`aws s3 sync "s3://%s/" /tmp/backup/ --endpoint-url "%s"`, s3Path, s3Source.Endpoint)
	}

	restoreScript := fmt.Sprintf(`#!/bin/sh
set -e
echo "Downloading backup from S3..."
%s
RESTORE_DIR="/tmp/backup"
if [ ! -f "${RESTORE_DIR}/manifest.json" ]; then
  SUBDIR=$(find "${RESTORE_DIR}" -maxdepth 1 -mindepth 1 -type d | sort -r | head -1)
  if [ -n "${SUBDIR}" ] && [ -f "${SUBDIR}/manifest.json" ]; then
    RESTORE_DIR="${SUBDIR}"
    echo "Found backup in subdirectory: ${RESTORE_DIR}"
  fi
fi
echo "Restoring data to PVC..."
hyperbytedb restore --input "${RESTORE_DIR}"
echo "Restore complete for ordinal %d"
`, syncCmd, ordinal)

	env := []corev1.EnvVar{}
	if s3Source.CredentialsSecretName != "" {
		env = append(env,
			corev1.EnvVar{
				Name: "AWS_ACCESS_KEY_ID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: s3Source.CredentialsSecretName},
						Key:                  "access_key_id",
					},
				},
			},
			corev1.EnvVar{
				Name: "AWS_SECRET_ACCESS_KEY",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: s3Source.CredentialsSecretName},
						Key:                  "secret_access_key",
					},
				},
			},
		)
	}
	if s3Source.Region != "" {
		env = append(env, corev1.EnvVar{Name: "AWS_DEFAULT_REGION", Value: s3Source.Region})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-restore-%d", restore.Name, ordinal),
			Namespace: restore.Namespace,
			Labels:    hyperbytedb.CommonLabels(cluster),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(2)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:    "restore",
							Image:   image,
							Command: []string{"sh", "-c", restoreScript},
							Env:     env,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/var/lib/hyperbytedb"},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: fmt.Sprintf("data-%s-%d", cluster.Name, ordinal),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *HyperbytedbRestoreReconciler) setRestorePhase(ctx context.Context, restore *hyperbytedbv1alpha1.HyperbytedbRestore, phase hyperbytedbv1alpha1.RestorePhase, reason, message string) {
	restore.Status.Phase = phase
	meta.SetStatusCondition(&restore.Status.Conditions, metav1.Condition{
		Type:               string(phase),
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.Now(),
	})
	_ = r.Status().Update(ctx, restore)
}

// SetupWithManager sets up the controller with the Manager.
func (r *HyperbytedbRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hyperbytedbv1alpha1.HyperbytedbRestore{}).
		Owns(&batchv1.Job{}).
		Named("hyperbytedbrestore").
		Complete(r)
}
