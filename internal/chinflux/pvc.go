package chinflux

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/chinflux/chinflux-operator/api/v1alpha1"
)

// CleanupOrphanPVCs deletes PVCs for ordinals >= desiredReplicas.
// This prevents storage leaks after a scale-down.
func CleanupOrphanPVCs(
	ctx context.Context,
	c client.Client,
	cluster *v1alpha1.ChinfluxCluster,
	currentReplicas, desiredReplicas int32,
) error {
	logger := log.FromContext(ctx)
	stsName := StatefulSetName(cluster)

	for i := desiredReplicas; i < currentReplicas; i++ {
		pvcName := fmt.Sprintf("data-%s-%d", stsName, i)
		pvc := &corev1.PersistentVolumeClaim{}
		err := c.Get(ctx, types.NamespacedName{
			Name:      pvcName,
			Namespace: cluster.Namespace,
		}, pvc)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("looking up PVC %s: %w", pvcName, err)
		}

		logger.Info("Deleting orphan PVC after scale-down", "pvc", pvcName)
		if err := c.Delete(ctx, pvc); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting PVC %s: %w", pvcName, err)
		}
	}
	return nil
}
