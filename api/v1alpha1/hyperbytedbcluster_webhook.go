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

package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// HyperbytedbClusterWebhook implements defaulting and validation.
type HyperbytedbClusterWebhook struct{}

func SetupHyperbytedbClusterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &HyperbytedbCluster{}).
		WithDefaulter(&HyperbytedbClusterWebhook{}).
		WithValidator(&HyperbytedbClusterWebhook{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-hyperbytedb-hyperbytedb-io-v1alpha1-hyperbytedbcluster,mutating=true,failurePolicy=fail,sideEffects=None,groups=hyperbytedb.hyperbytedb.io,resources=hyperbytedbclusters,verbs=create;update,versions=v1alpha1,name=mhyperbytedbcluster.kb.io,admissionReviewVersions=v1

func (w *HyperbytedbClusterWebhook) Default(_ context.Context, obj *HyperbytedbCluster) error {
	if obj.Spec.Replicas == nil {
		obj.Spec.Replicas = ptr.To(int32(1))
	}

	if obj.Spec.Image == "" {
		obj.Spec.Image = "hyperbytedb:latest"
	}

	if obj.Spec.Server.Port == 0 {
		obj.Spec.Server.Port = 8086
	}

	if obj.Spec.Storage.VolumeClaimTemplate == nil {
		obj.Spec.Storage.VolumeClaimTemplate = &PersistentVolumeClaimSpec{
			Size: resource.MustParse("10Gi"),
		}
	}

	if obj.Spec.Logging.Level == "" {
		obj.Spec.Logging.Level = "info"
	}
	if obj.Spec.Logging.Format == "" {
		obj.Spec.Logging.Format = "text"
	}

	if obj.Spec.Failover == nil {
		obj.Spec.Failover = &FailoverSpec{
			Enabled:             true,
			MaxFailoverCount:    1,
			FailoverTimeoutSecs: 300,
		}
	}

	if obj.Spec.Flush.TimeBucketDuration == "" {
		obj.Spec.Flush.TimeBucketDuration = "1h"
	}

	if obj.Spec.ChDB.PoolSize == 0 {
		obj.Spec.ChDB.PoolSize = 1
	}

	if obj.Spec.Retention.Interval == "" {
		obj.Spec.Retention.Interval = "60s"
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-hyperbytedb-hyperbytedb-io-v1alpha1-hyperbytedbcluster,mutating=false,failurePolicy=fail,sideEffects=None,groups=hyperbytedb.hyperbytedb.io,resources=hyperbytedbclusters,verbs=create;update,versions=v1alpha1,name=vhyperbytedbcluster.kb.io,admissionReviewVersions=v1

func (w *HyperbytedbClusterWebhook) ValidateCreate(_ context.Context, obj *HyperbytedbCluster) (admission.Warnings, error) {
	return validateCluster(obj)
}

func (w *HyperbytedbClusterWebhook) ValidateUpdate(_ context.Context, _ *HyperbytedbCluster, newObj *HyperbytedbCluster) (admission.Warnings, error) {
	return validateCluster(newObj)
}

func (w *HyperbytedbClusterWebhook) ValidateDelete(_ context.Context, _ *HyperbytedbCluster) (admission.Warnings, error) {
	return nil, nil
}

func validateCluster(cluster *HyperbytedbCluster) (admission.Warnings, error) {
	replicas := int32(1)
	if cluster.Spec.Replicas != nil {
		replicas = *cluster.Spec.Replicas
	}

	if replicas < 1 {
		return nil, fmt.Errorf("replicas must be at least 1, got %d", replicas)
	}

	if cluster.Spec.Server.Port != 0 && (cluster.Spec.Server.Port < 1 || cluster.Spec.Server.Port > 65535) {
		return nil, fmt.Errorf("server port must be between 1 and 65535, got %d", cluster.Spec.Server.Port)
	}

	if cluster.Spec.Autoscaling != nil && cluster.Spec.Autoscaling.Enabled {
		if cluster.Spec.Autoscaling.MaxReplicas < 1 {
			return nil, fmt.Errorf("autoscaling.maxReplicas must be at least 1")
		}
		if cluster.Spec.Autoscaling.MinReplicas > cluster.Spec.Autoscaling.MaxReplicas {
			return nil, fmt.Errorf("autoscaling.minReplicas (%d) must be <= maxReplicas (%d)",
				cluster.Spec.Autoscaling.MinReplicas, cluster.Spec.Autoscaling.MaxReplicas)
		}
	}

	if cluster.Spec.Failover != nil && cluster.Spec.Failover.Enabled {
		if cluster.Spec.Failover.FailoverTimeoutSecs > 0 && cluster.Spec.Failover.FailoverTimeoutSecs < 60 {
			return nil, fmt.Errorf("failover.failoverTimeoutSecs must be at least 60, got %d",
				cluster.Spec.Failover.FailoverTimeoutSecs)
		}
		if cluster.Spec.Failover.MaxFailoverCount < 1 {
			return nil, fmt.Errorf("failover.maxFailoverCount must be at least 1")
		}
	}

	if cluster.Spec.Server.TLS != nil && cluster.Spec.Server.TLS.Enabled {
		if cluster.Spec.Server.TLS.CertManagerIssuerRef != nil {
			ref := cluster.Spec.Server.TLS.CertManagerIssuerRef
			if ref.Name == "" {
				return nil, fmt.Errorf("server.tls.certManagerIssuerRef.name must not be empty")
			}
			if ref.Kind != "Issuer" && ref.Kind != "ClusterIssuer" {
				return nil, fmt.Errorf("server.tls.certManagerIssuerRef.kind must be 'Issuer' or 'ClusterIssuer'")
			}
		}
	}

	var warnings admission.Warnings
	if replicas == 2 {
		warnings = append(warnings, "2-node clusters cannot tolerate any node failure; consider 3+ replicas")
	}

	return warnings, nil
}
