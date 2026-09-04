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

// HyperbytedbBackupReconciler reconciles a HyperbytedbBackup object.
type HyperbytedbBackupReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbbackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbbackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=hyperbytedb.hyperbyte.cloud,resources=hyperbytedbbackups/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

func (r *HyperbytedbBackupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	backup := &hyperbytedbv1alpha1.HyperbytedbBackup{}
	if err := r.Get(ctx, req.NamespacedName, backup); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      backup.Spec.ClusterName,
		Namespace: backup.Namespace,
	}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			log.Error(err, "Referenced cluster not found", "cluster", backup.Spec.ClusterName)
			backup.Status.Phase = hyperbytedbv1alpha1.BackupPhaseFailed
			meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
				Type:               "Ready",
				Status:             metav1.ConditionFalse,
				Reason:             "ClusterNotFound",
				Message:            fmt.Sprintf("Cluster %s not found", backup.Spec.ClusterName),
				LastTransitionTime: metav1.Now(),
			})
			_ = r.Status().Update(ctx, backup)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if backup.Spec.Schedule != "" {
		return r.reconcileCronJob(ctx, backup, cluster)
	}
	return r.reconcileJob(ctx, backup, cluster)
}

func (r *HyperbytedbBackupReconciler) reconcileJob(ctx context.Context, backup *hyperbytedbv1alpha1.HyperbytedbBackup, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) (ctrl.Result, error) {
	jobName := backup.Name

	existing := &batchv1.Job{}
	err := r.Get(ctx, types.NamespacedName{Name: jobName, Namespace: backup.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if backup.Status.Phase == hyperbytedbv1alpha1.BackupPhaseCompleted ||
			backup.Status.Phase == hyperbytedbv1alpha1.BackupPhaseFailed {
			return ctrl.Result{}, nil
		}

		job := r.buildBackupJob(backup, cluster, jobName)
		if err := controllerutil.SetControllerReference(backup, job, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		if err := r.Create(ctx, job); err != nil {
			return ctrl.Result{}, err
		}

		now := metav1.Now()
		backup.Status.Phase = hyperbytedbv1alpha1.BackupPhaseRunning
		backup.Status.StartTime = &now
		r.Recorder.Event(backup, corev1.EventTypeNormal, "BackupStarted", "Backup job created")
		_ = r.Status().Update(ctx, backup)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	if existing.Status.Succeeded > 0 {
		now := metav1.Now()
		backup.Status.Phase = hyperbytedbv1alpha1.BackupPhaseCompleted
		backup.Status.CompletionTime = &now
		backup.Status.BackupPath = r.buildBackupS3Path(backup)
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:               "Complete",
			Status:             metav1.ConditionTrue,
			Reason:             "BackupSucceeded",
			Message:            "Backup completed successfully",
			LastTransitionTime: metav1.Now(),
		})
		r.Recorder.Event(backup, corev1.EventTypeNormal, "BackupCompleted", "Backup completed successfully")
		_ = r.Status().Update(ctx, backup)
		return ctrl.Result{}, nil
	}

	if existing.Status.Failed > 0 {
		backup.Status.Phase = hyperbytedbv1alpha1.BackupPhaseFailed
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:               "Complete",
			Status:             metav1.ConditionFalse,
			Reason:             "BackupFailed",
			Message:            "Backup job failed",
			LastTransitionTime: metav1.Now(),
		})
		r.Recorder.Event(backup, corev1.EventTypeWarning, "BackupFailed", "Backup job failed")
		_ = r.Status().Update(ctx, backup)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *HyperbytedbBackupReconciler) reconcileCronJob(ctx context.Context, backup *hyperbytedbv1alpha1.HyperbytedbBackup, cluster *hyperbytedbv1alpha1.HyperbytedbCluster) (ctrl.Result, error) {
	cronJobName := backup.Name

	desired := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: backup.Namespace,
			Labels:    hyperbytedb.CommonLabels(cluster),
		},
		Spec: batchv1.CronJobSpec{
			Schedule: backup.Spec.Schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: r.buildBackupJob(backup, cluster, cronJobName).Spec,
			},
			SuccessfulJobsHistoryLimit: ptr.To(int32(3)),
			FailedJobsHistoryLimit:     ptr.To(int32(3)),
		},
	}

	if err := controllerutil.SetControllerReference(backup, desired, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	existing := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{Name: cronJobName, Namespace: backup.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, desired); err != nil {
			return ctrl.Result{}, err
		}
		backup.Status.Phase = hyperbytedbv1alpha1.BackupPhaseRunning
		meta.SetStatusCondition(&backup.Status.Conditions, metav1.Condition{
			Type:               "Scheduled",
			Status:             metav1.ConditionTrue,
			Reason:             "CronJobCreated",
			Message:            fmt.Sprintf("Scheduled backup with cron: %s", backup.Spec.Schedule),
			LastTransitionTime: metav1.Now(),
		})
		r.Recorder.Eventf(backup, corev1.EventTypeNormal, "Scheduled",
			"Created CronJob with schedule %s", backup.Spec.Schedule)
		_ = r.Status().Update(ctx, backup)
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, err
	}

	existing.Spec.Schedule = desired.Spec.Schedule
	existing.Spec.JobTemplate = desired.Spec.JobTemplate
	return ctrl.Result{}, r.Update(ctx, existing)
}

func (r *HyperbytedbBackupReconciler) buildBackupJob(backup *hyperbytedbv1alpha1.HyperbytedbBackup, cluster *hyperbytedbv1alpha1.HyperbytedbCluster, name string) *batchv1.Job {
	s3 := backup.Spec.Destination.S3
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: backup.Namespace,
			Labels:    hyperbytedb.CommonLabels(cluster),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(3)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:  corev1.RestartPolicyOnFailure,
					InitContainers: []corev1.Container{snapshotBackupInitContainer(hyperbytedb.ResolveHyperbytedbImage(cluster))},
					Containers:     []corev1.Container{mcUploadContainer(s3, backup.Spec.RetentionDays)},
					Volumes: []corev1.Volume{
						dataVolume(cluster.Name, 0, true),
						snapshotVolume(),
					},
				},
			},
		},
	}
}

func (r *HyperbytedbBackupReconciler) buildBackupS3Path(backup *hyperbytedbv1alpha1.HyperbytedbBackup) string {
	s3 := backup.Spec.Destination.S3
	path := s3.Bucket
	if s3.Prefix != "" {
		path += "/" + strings.TrimSuffix(s3.Prefix, "/")
	}
	return path
}

// SetupWithManager sets up the controller with the Manager.
func (r *HyperbytedbBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hyperbytedbv1alpha1.HyperbytedbBackup{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Named("hyperbytedbbackup").
		Complete(r)
}
