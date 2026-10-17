package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func departedPod(name string, ready, deleting bool) *corev1.Pod {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}}
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}
	if deleting {
		now := metav1.Now()
		pod.DeletionTimestamp = &now
	}
	return pod
}

// The 12 -> 3 stall: pod 3 still ready, pods 4.. unready, one already going.
func TestStuckDepartedPodsAreTheUnreadyOnes(t *testing.T) {
	pods := []*corev1.Pod{
		departedPod("db-3", true, false),
		departedPod("db-4", false, false),
		departedPod("db-5", false, true),
		departedPod("db-11", false, false),
	}
	got := stuckDepartedPods(pods)
	if len(got) != 2 || got[0].Name != "db-4" || got[1].Name != "db-11" {
		names := make([]string, 0, len(got))
		for _, p := range got {
			names = append(names, p.Name)
		}
		t.Fatalf("stuck pods = %v, want [db-4 db-11]", names)
	}
}

func TestNoStuckPodsWhenAllDepartedPodsAreReady(t *testing.T) {
	if got := stuckDepartedPods([]*corev1.Pod{departedPod("db-3", true, false)}); len(got) != 0 {
		t.Fatalf("got %d stuck pods, want 0", len(got))
	}
}
