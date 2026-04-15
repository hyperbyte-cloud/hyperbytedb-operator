package hyperbytedb

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

func PeersConfigMapName(cluster *v1alpha1.HyperbytedbCluster) string {
	return cluster.Name + "-peers"
}

// BuildPeersConfigMap creates a ConfigMap that stores the current cluster
// membership as a simple key-value map:
//
//	ordinal "0" -> "hyperbytedb-0.hyperbytedb-headless.ns.svc.cluster.local:8086"
//	ordinal "1" -> "hyperbytedb-1.hyperbytedb-headless.ns.svc.cluster.local:8086"
//
// The init container reads this to derive its own identity and peer list.
func BuildPeersConfigMap(cluster *v1alpha1.HyperbytedbCluster, namespace string, replicas int32) *corev1.ConfigMap {
	port := serverPort(cluster)
	stsName := StatefulSetName(cluster)
	headlessSvc := HeadlessServiceName(cluster)

	data := make(map[string]string)
	for i := int32(0); i < replicas; i++ {
		fqdn := fmt.Sprintf("%s-%d.%s.%s.svc.cluster.local:%d",
			stsName, i, headlessSvc, namespace, port)
		data[fmt.Sprintf("%d", i)] = fqdn
	}

	// Store replicas count for the init script
	data["replicas"] = fmt.Sprintf("%d", replicas)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PeersConfigMapName(cluster),
			Namespace: namespace,
			Labels:    CommonLabels(cluster),
		},
		Data: data,
	}
}

// PeerListFromConfigMap builds the comma-separated peer list for a given
// ordinal by reading all entries from the peers ConfigMap data, excluding
// the node's own address.
func PeerListFromConfigMap(data map[string]string, selfOrdinal int) string {
	selfKey := fmt.Sprintf("%d", selfOrdinal)
	peers := make([]string, 0, len(data))
	for k, v := range data {
		if k == "replicas" {
			continue
		}
		if k != selfKey {
			peers = append(peers, v)
		}
	}
	sort.Strings(peers)
	return strings.Join(peers, ",")
}
