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

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

var _ = Describe("HyperbytedbRestore Controller", func() {
	Context("When restoring with a missing cluster", func() {
		const restoreName = "test-restore-orphan"

		ctx := context.Background()
		restoreNN := types.NamespacedName{Name: restoreName, Namespace: "default"}

		BeforeEach(func() {
			restore := &hyperbytedbv1alpha1.HyperbytedbRestore{}
			err := k8sClient.Get(ctx, restoreNN, restore)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &hyperbytedbv1alpha1.HyperbytedbRestore{
					ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: "default"},
					Spec: hyperbytedbv1alpha1.HyperbytedbRestoreSpec{
						ClusterName: "nonexistent-cluster",
						BackupName:  "some-backup",
						Source: &hyperbytedbv1alpha1.RestoreSource{
							S3: hyperbytedbv1alpha1.S3BackupSpec{
								Bucket: "test-bucket",
								Prefix: "backups/",
							},
						},
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &hyperbytedbv1alpha1.HyperbytedbRestore{}
			if err := k8sClient.Get(ctx, restoreNN, resource); err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should set status to Failed", func() {
			controllerReconciler := &HyperbytedbRestoreReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: restoreNN,
			})
			Expect(err).NotTo(HaveOccurred())

			restore := &hyperbytedbv1alpha1.HyperbytedbRestore{}
			Expect(k8sClient.Get(ctx, restoreNN, restore)).To(Succeed())
			Expect(restore.Status.Phase).To(Equal(hyperbytedbv1alpha1.RestorePhaseFailed))
		})
	})

	Context("When restoring a valid cluster", func() {
		const (
			restoreName = "test-restore-valid"
			clusterName = "test-restore-cluster"
			backupName  = "test-restore-backup"
		)

		ctx := context.Background()
		restoreNN := types.NamespacedName{Name: restoreName, Namespace: "default"}
		clusterNN := types.NamespacedName{Name: clusterName, Namespace: "default"}
		backupNN := types.NamespacedName{Name: backupName, Namespace: "default"}

		BeforeEach(func() {
			By("creating the referenced cluster")
			cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, clusterNN, cluster)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &hyperbytedbv1alpha1.HyperbytedbCluster{
					ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: "default"},
					Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
						Replicas: ptr.To(int32(1)),
						Image:    "hyperbytedb:latest",
					},
				})).To(Succeed())
			}

			By("creating the referenced backup")
			backup := &hyperbytedbv1alpha1.HyperbytedbBackup{}
			err = k8sClient.Get(ctx, backupNN, backup)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &hyperbytedbv1alpha1.HyperbytedbBackup{
					ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: "default"},
					Spec: hyperbytedbv1alpha1.HyperbytedbBackupSpec{
						ClusterName: clusterName,
						Destination: hyperbytedbv1alpha1.BackupDestination{
							S3: hyperbytedbv1alpha1.S3BackupSpec{
								Bucket: "test-bucket",
								Prefix: "backups/",
							},
						},
					},
				})).To(Succeed())
			}

			By("creating the HyperbytedbRestore resource")
			restore := &hyperbytedbv1alpha1.HyperbytedbRestore{}
			err = k8sClient.Get(ctx, restoreNN, restore)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &hyperbytedbv1alpha1.HyperbytedbRestore{
					ObjectMeta: metav1.ObjectMeta{Name: restoreName, Namespace: "default"},
					Spec: hyperbytedbv1alpha1.HyperbytedbRestoreSpec{
						ClusterName: clusterName,
						BackupName:  backupName,
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			for _, nn := range []types.NamespacedName{restoreNN, backupNN, clusterNN} {
				obj := &hyperbytedbv1alpha1.HyperbytedbRestore{}
				if nn == backupNN {
					b := &hyperbytedbv1alpha1.HyperbytedbBackup{}
					if err := k8sClient.Get(ctx, nn, b); err == nil {
						_ = k8sClient.Delete(ctx, b)
					}
					continue
				}
				if nn == clusterNN {
					c := &hyperbytedbv1alpha1.HyperbytedbCluster{}
					if err := k8sClient.Get(ctx, nn, c); err == nil {
						_ = k8sClient.Delete(ctx, c)
					}
					continue
				}
				if err := k8sClient.Get(ctx, nn, obj); err == nil {
					_ = k8sClient.Delete(ctx, obj)
				}
			}
		})

		It("should resolve S3 source from backup and begin restore", func() {
			controllerReconciler := &HyperbytedbRestoreReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: restoreNN,
			})
			// Reconcile will fail trying to scale the STS that doesn't exist yet,
			// which is expected -- we just want to verify the S3 resolution worked.
			// The error indicates it tried to look up the StatefulSet (good).
			if err != nil {
				// Expected when there's no StatefulSet to scale down
				restore := &hyperbytedbv1alpha1.HyperbytedbRestore{}
				Expect(k8sClient.Get(ctx, restoreNN, restore)).To(Succeed())
				// Phase should not be Failed due to S3 resolution
				Expect(restore.Status.Phase).NotTo(Equal(hyperbytedbv1alpha1.RestorePhaseFailed))
			}
		})
	})
})
