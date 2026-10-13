package controller

import "testing"

// A pod the leader still lists as departed must be sent add-node, or a node
// scaled down and back up keeps its persisted `leaving` state and never passes
// readiness. Anything else is a current member and must be left alone.
func TestIsDepartedState(t *testing.T) {
	for _, s := range []string{"leaving", "decommissioning"} {
		if !isDepartedState(s) {
			t.Errorf("%q must count as departed so the node is re-added", s)
		}
	}
	for _, s := range []string{"active", "syncing", "joining", "draining", "disconnected", ""} {
		if isDepartedState(s) {
			t.Errorf("%q is a current member and must not be re-added", s)
		}
	}
}
