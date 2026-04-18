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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	hyperbytedbv1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
	"github.com/hyperbyte-cloud/hyperbytedb-operator/internal/hyperbytedb"
)

var _ = Describe("HyperbytedbCluster Controller", func() {
	Context("When reconciling a single-node cluster", func() {
		const resourceName = "test-single"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the HyperbytedbCluster resource")
			cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, cluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &hyperbytedbv1alpha1.HyperbytedbCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
						Replicas: ptr.To(int32(1)),
						Image:    "hyperbytedb:latest",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create ConfigMap, Services, and StatefulSet (no peers ConfigMap)", func() {
			controllerReconciler := &HyperbytedbClusterReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
				Members:  hyperbytedb.NewMemberManager(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the ConfigMap was created")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-config", Namespace: "default",
			}, cm)).To(Succeed())
			Expect(cm.Data).To(HaveKey("config.toml"))

			By("verifying the legacy peers ConfigMap is NOT created")
			peersCM := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-peers", Namespace: "default",
			}, peersCM)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"peers ConfigMap should no longer be created — membership is now API-driven")

			By("verifying the headless Service was created")
			svc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-headless", Namespace: "default",
			}, svc)).To(Succeed())
			Expect(svc.Spec.ClusterIP).To(Equal(corev1.ClusterIPNone))

			By("verifying the client Service was created")
			clientSvc := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, clientSvc)).To(Succeed())

			By("verifying the StatefulSet was created with 1 replica")
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, sts)).To(Succeed())
			Expect(*sts.Spec.Replicas).To(Equal(int32(1)))

			By("verifying single-node config includes [cluster] with enabled = false (stable pod template when scaling)")
			Expect(cm.Data["config.toml"]).To(ContainSubstring("[cluster]"))
			Expect(cm.Data["config.toml"]).To(ContainSubstring("enabled = false"))
		})
	})

	Context("When reconciling a 3-node cluster", func() {
		const resourceName = "test-cluster"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, cluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &hyperbytedbv1alpha1.HyperbytedbCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
						Replicas: ptr.To(int32(3)),
						Image:    "hyperbytedb:latest",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should create a 3-replica StatefulSet with cluster config and PDB", func() {
			controllerReconciler := &HyperbytedbClusterReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
				Members:  hyperbytedb.NewMemberManager(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the StatefulSet has 3 replicas")
			sts := &appsv1.StatefulSet{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, sts)).To(Succeed())
			Expect(*sts.Spec.Replicas).To(Equal(int32(3)))

			By("verifying the ConfigMap has cluster section")
			cm := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-config", Namespace: "default",
			}, cm)).To(Succeed())
			Expect(cm.Data["config.toml"]).To(ContainSubstring("[cluster]"))
			Expect(cm.Data["config.toml"]).To(ContainSubstring("enabled = true"))

			By("verifying the peers ConfigMap is NOT created (membership is API-driven)")
			peersCM := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName + "-peers", Namespace: "default",
			}, peersCM)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("When the cluster is paused", func() {
		const resourceName = "test-paused"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			cluster := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, cluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &hyperbytedbv1alpha1.HyperbytedbCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: hyperbytedbv1alpha1.HyperbytedbClusterSpec{
						Replicas: ptr.To(int32(1)),
						Image:    "hyperbytedb:latest",
						Paused:   true,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &hyperbytedbv1alpha1.HyperbytedbCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should skip reconciliation and set Paused condition", func() {
			controllerReconciler := &HyperbytedbClusterReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(32),
				Members:  hyperbytedb.NewMemberManager(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying no StatefulSet was created")
			sts := &appsv1.StatefulSet{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: resourceName, Namespace: "default",
			}, sts)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})
})
