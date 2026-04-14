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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	chinfluxv1alpha1 "github.com/chinflux/chinflux-operator/api/v1alpha1"
)

var _ = Describe("ChinfluxBackup Controller", func() {
	Context("When reconciling a one-shot backup", func() {
		const (
			backupName  = "test-backup"
			clusterName = "test-backup-cluster"
		)

		ctx := context.Background()

		backupNN := types.NamespacedName{Name: backupName, Namespace: "default"}
		clusterNN := types.NamespacedName{Name: clusterName, Namespace: "default"}

		BeforeEach(func() {
			By("creating the referenced cluster")
			cluster := &chinfluxv1alpha1.ChinfluxCluster{}
			err := k8sClient.Get(ctx, clusterNN, cluster)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &chinfluxv1alpha1.ChinfluxCluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: "default"},
					Spec: chinfluxv1alpha1.ChinfluxClusterSpec{
						Replicas: ptr.To(int32(1)),
						Image:    "chinflux:latest",
					},
				})).To(Succeed())
			}

			By("creating the ChinfluxBackup resource")
			backup := &chinfluxv1alpha1.ChinfluxBackup{}
			err = k8sClient.Get(ctx, backupNN, backup)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &chinfluxv1alpha1.ChinfluxBackup{
					ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: "default"},
					Spec: chinfluxv1alpha1.ChinfluxBackupSpec{
						ClusterName: clusterName,
						Destination: chinfluxv1alpha1.BackupDestination{
							S3: chinfluxv1alpha1.S3BackupSpec{
								Bucket: "test-bucket",
								Prefix: "backups/",
								Region: "us-east-1",
							},
						},
						RetentionDays: 7,
						BackupType:    "full",
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &chinfluxv1alpha1.ChinfluxBackup{}
			if err := k8sClient.Get(ctx, backupNN, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
			cluster := &chinfluxv1alpha1.ChinfluxCluster{}
			if err := k8sClient.Get(ctx, clusterNN, cluster); err == nil {
				Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
			}
		})

		It("should create a backup Job", func() {
			controllerReconciler := &ChinfluxBackupReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: backupNN,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the backup status is Running")
			backup := &chinfluxv1alpha1.ChinfluxBackup{}
			Expect(k8sClient.Get(ctx, backupNN, backup)).To(Succeed())
			Expect(backup.Status.Phase).To(Equal(chinfluxv1alpha1.BackupPhaseRunning))
			Expect(backup.Status.StartTime).NotTo(BeNil())
		})
	})

	Context("When the referenced cluster does not exist", func() {
		const backupName = "test-backup-orphan"

		ctx := context.Background()
		backupNN := types.NamespacedName{Name: backupName, Namespace: "default"}

		BeforeEach(func() {
			backup := &chinfluxv1alpha1.ChinfluxBackup{}
			err := k8sClient.Get(ctx, backupNN, backup)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &chinfluxv1alpha1.ChinfluxBackup{
					ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: "default"},
					Spec: chinfluxv1alpha1.ChinfluxBackupSpec{
						ClusterName: "nonexistent-cluster",
						Destination: chinfluxv1alpha1.BackupDestination{
							S3: chinfluxv1alpha1.S3BackupSpec{
								Bucket: "test-bucket",
							},
						},
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &chinfluxv1alpha1.ChinfluxBackup{}
			if err := k8sClient.Get(ctx, backupNN, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set status to Failed", func() {
			controllerReconciler := &ChinfluxBackupReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: backupNN,
			})
			Expect(err).NotTo(HaveOccurred())

			backup := &chinfluxv1alpha1.ChinfluxBackup{}
			Expect(k8sClient.Get(ctx, backupNN, backup)).To(Succeed())
			Expect(backup.Status.Phase).To(Equal(chinfluxv1alpha1.BackupPhaseFailed))
		})
	})
})
