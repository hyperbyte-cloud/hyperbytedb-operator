package hyperbytedb

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/hyperbyte-cloud/hyperbytedb-operator/api/v1alpha1"
)

// MemberManager queries live cluster pods to build the authoritative
// membership view and detect new, desynced, or failed members.
type MemberManager struct {
	Client *Client
}

func NewMemberManager() *MemberManager {
	return &MemberManager{
		Client: NewClient(),
	}
}

// CollectMemberStatuses queries every pod in the StatefulSet and returns
// per-member status. Pods that fail to respond are marked unhealthy.
func (m *MemberManager) CollectMemberStatuses(
	ctx context.Context,
	cluster *v1alpha1.HyperbytedbCluster,
	namespace string,
	replicas int32,
) []v1alpha1.MemberStatus {
	logger := log.FromContext(ctx)
	port := serverPort(cluster)
	stsName := StatefulSetName(cluster)
	headlessSvc := HeadlessServiceName(cluster)

	members := make([]v1alpha1.MemberStatus, 0, replicas)

	for i := int32(0); i < replicas; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		host := fmt.Sprintf("%s.%s.%s.svc.cluster.local",
			podName, headlessSvc, namespace)

		ms := v1alpha1.MemberStatus{
			Name:               fmt.Sprintf("%s-%d", cluster.Name, i),
			NodeID:             i + 1,
			PodName:            podName,
			State:              "Unknown",
			Health:             false,
			LastTransitionTime: metav1.Now(),
		}

		hs, err := m.Client.GetNodeHealth(ctx, host, port)
		if err != nil {
			logger.V(1).Info("Could not reach member", "pod", podName, "error", err)
			ms.State = "Unreachable"
			members = append(members, ms)
			continue
		}

		ms.State = hs.Status
		ms.Health = hs.Status == "pass" || hs.Status == "Active" || hs.Status == "active"
		members = append(members, ms)
	}

	return members
}

// DeriveClusterState computes the high-level cluster health from individual
// member statuses.
func DeriveClusterState(members []v1alpha1.MemberStatus) string {
	if len(members) == 0 {
		return "Unknown"
	}

	healthy := 0
	syncing := 0
	for _, m := range members {
		if m.Health {
			healthy++
		} else {
			switch m.State {
			case "Syncing", "Joining", "syncing", "joining":
				syncing++
			}
		}
	}

	if healthy == len(members) {
		return "Healthy"
	}
	if syncing > 0 && healthy+syncing == len(members) {
		return "Recovering"
	}
	if healthy > 0 {
		return "Degraded"
	}
	return "Unavailable"
}

// FindUnhealthyMembers returns members that are not in the Active state.
func FindUnhealthyMembers(members []v1alpha1.MemberStatus) []v1alpha1.MemberStatus {
	var unhealthy []v1alpha1.MemberStatus
	for _, m := range members {
		if !m.Health {
			unhealthy = append(unhealthy, m)
		}
	}
	return unhealthy
}
