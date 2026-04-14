package chinflux

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/chinflux/chinflux-operator/api/v1alpha1"
)

// BuildServiceMonitor returns an unstructured ServiceMonitor so we don't need
// to import the prometheus-operator API types as a hard dependency.
func BuildServiceMonitor(cluster *v1alpha1.ChinfluxCluster) *unstructured.Unstructured {
	labels := CommonLabels(cluster)

	sm := &unstructured.Unstructured{}
	sm.SetAPIVersion("monitoring.coreos.com/v1")
	sm.SetKind("ServiceMonitor")
	sm.SetName(cluster.Name)
	sm.SetNamespace(cluster.Namespace)
	sm.SetLabels(labels)

	sm.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": convertLabels(labels),
		},
		"endpoints": []interface{}{
			map[string]interface{}{
				"port":     "http",
				"path":     "/metrics",
				"interval": "15s",
			},
		},
		"namespaceSelector": map[string]interface{}{
			"matchNames": []interface{}{cluster.Namespace},
		},
	}

	return sm
}

// ServiceMonitorGVR returns the GroupVersionResource for ServiceMonitor.
func ServiceMonitorGVR() metav1.GroupVersionResource {
	return metav1.GroupVersionResource{
		Group:    "monitoring.coreos.com",
		Version:  "v1",
		Resource: "servicemonitors",
	}
}

func convertLabels(labels map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(labels))
	for k, v := range labels {
		result[k] = v
	}
	return result
}
