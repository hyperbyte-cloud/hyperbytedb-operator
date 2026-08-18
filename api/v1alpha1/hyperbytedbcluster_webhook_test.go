package v1alpha1

import (
	"strings"
	"testing"

	"k8s.io/utils/ptr"
)

func TestValidateSharding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		replicas int32
		sharding *ShardingSpec
		wantErr  string
	}{
		{name: "nil sharding", replicas: 1},
		{
			name:     "enabled requires cluster",
			replicas: 1,
			sharding: &ShardingSpec{Enabled: true},
			wantErr:  "replicas > 1",
		},
		{
			name:     "enabled on 3 replicas",
			replicas: 3,
			sharding: &ShardingSpec{Enabled: true, ReplicationFactor: 2},
		},
		{
			name:     "merge not less than split",
			replicas: 3,
			sharding: &ShardingSpec{
				Enabled:           true,
				RegionMergeSeries: 10,
				RegionSplitSeries: 5,
			},
			wantErr: "regionMergeSeries",
		},
		{
			name:     "split not less than max",
			replicas: 3,
			sharding: &ShardingSpec{
				Enabled:           true,
				RegionSplitSeries: 10,
				RegionMaxSeries:   10,
			},
			wantErr: "regionSplitSeries",
		},
		{
			name:     "kind split-test thresholds",
			replicas: 6,
			sharding: &ShardingSpec{
				Enabled:           true,
				RegionSplitSeries: 5,
				RegionMaxSeries:   10,
				RegionMergeSeries: 2,
			},
		},
		{
			name:     "disabled on single replica is ok",
			replicas: 1,
			sharding: &ShardingSpec{Enabled: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cluster := &HyperbytedbCluster{
				Spec: HyperbytedbClusterSpec{
					Replicas: ptr.To(tt.replicas),
					Sharding: tt.sharding,
				},
			}
			err := validateSharding(cluster, tt.replicas)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}
