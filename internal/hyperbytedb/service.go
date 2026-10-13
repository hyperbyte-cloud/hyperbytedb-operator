package hyperbytedb

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func HeadlessServiceName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name + "-headless"
}

func ClientServiceName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name
}

func serverPort(cluster *v1alpha1.HyperbytedbCluster) int32 {
	return ServerPort(cluster)
}

// ServerPort returns the configured HTTP port or the default 8086.
func ServerPort(cluster *v1alpha1.HyperbytedbCluster) int32 {
	if cluster.Spec.Server.Port > 0 {
		return cluster.Spec.Server.Port
	}
	return 8086
}

// ControlPort is the node's control listener: liveness and Raft RPCs,
// served on a dedicated runtime in the database so write saturation cannot
// starve them. Always the server port + 1.
func ControlPort(cluster *v1alpha1.HyperbytedbCluster) int32 {
	return ServerPort(cluster) + 1
}

// ControlListenerAnnotation opts a cluster into the control listener.
const ControlListenerAnnotation = "hyperbytedb.hyperbyte.cloud/control-listener"

// ControlListenerEnabled reports whether this cluster's pods get
// control_port and a liveness probe on it. Opt-in, because only database
// builds with the control listener serve that port: turning it on for every
// clustered CR left an existing 0.8.5-beta cluster failing liveness on a
// port nothing listened on, restarting in a loop.
func ControlListenerEnabled(cluster *v1alpha1.HyperbytedbCluster, clusterEnabled bool) bool {
	return clusterEnabled && cluster.Annotations[ControlListenerAnnotation] == "true" &&
		Supports(cluster, CapabilityControlListener)
}

func BuildHeadlessService(cluster *v1alpha1.HyperbytedbCluster) *corev1.Service {
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

func BuildClientService(cluster *v1alpha1.HyperbytedbCluster) *corev1.Service {
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
