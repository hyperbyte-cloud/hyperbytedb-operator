package chinflux

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/chinflux/chinflux-operator/api/v1alpha1"
)

func HeadlessServiceName(cluster *v1alpha1.ChinfluxCluster) string {
	return cluster.Name + "-headless"
}

func ClientServiceName(cluster *v1alpha1.ChinfluxCluster) string {
	return cluster.Name
}

func serverPort(cluster *v1alpha1.ChinfluxCluster) int32 {
	return ServerPort(cluster)
}

// ServerPort returns the configured HTTP port or the default 8086.
func ServerPort(cluster *v1alpha1.ChinfluxCluster) int32 {
	if cluster.Spec.Server.Port > 0 {
		return cluster.Spec.Server.Port
	}
	return 8086
}

func BuildHeadlessService(cluster *v1alpha1.ChinfluxCluster) *corev1.Service {
	port := serverPort(cluster)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HeadlessServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    CommonLabels(cluster),
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:                corev1.ClusterIPNone,
			Selector:                 CommonLabels(cluster),
			PublishNotReadyAddresses: true,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func BuildClientService(cluster *v1alpha1.ChinfluxCluster) *corev1.Service {
	port := serverPort(cluster)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClientServiceName(cluster),
			Namespace: cluster.Namespace,
			Labels:    CommonLabels(cluster),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: CommonLabels(cluster),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       port,
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
