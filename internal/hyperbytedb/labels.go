package hyperbytedb

import (
	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func CommonLabels(cluster *v1alpha1.HyperbytedbCluster) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "hyperbytedb",
		"app.kubernetes.io/instance":   cluster.Name,
		"app.kubernetes.io/managed-by": "hyperbytedb-operator",
	}
}
