package chinflux

import (
	v1alpha1 "github.com/chinflux/chinflux-operator/api/v1alpha1"
)

func CommonLabels(cluster *v1alpha1.ChinfluxCluster) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "chinflux",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/managed-by": "chinflux-operator",
	}
}
