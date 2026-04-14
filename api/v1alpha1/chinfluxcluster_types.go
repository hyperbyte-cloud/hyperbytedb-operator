/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChinfluxClusterSpec defines the desired state of ChinfluxCluster.
type ChinfluxClusterSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	Replicas *int32 `json:"replicas,omitempty"`

	// +kubebuilder:default="chinflux:latest"
	Image string `json:"image,omitempty"`

	// Explicit version tag for upgrade orchestration. When changed the
	// operator performs a rolling upgrade one pod at a time.
	// +optional
	Version string `json:"version,omitempty"`

	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// When true the operator skips reconciliation entirely, allowing
	// manual maintenance on the cluster.
	// +optional
	Paused bool `json:"paused,omitempty"`

	// +optional
	Server ServerSpec `json:"server,omitempty"`

	// +optional
	Storage StorageSpec `json:"storage,omitempty"`

	// +optional
	Flush FlushSpec `json:"flush,omitempty"`

	// +optional
	Compaction CompactionSpec `json:"compaction,omitempty"`

	// +optional
	ChDB ChDBSpec `json:"chdb,omitempty"`

	// +optional
	Auth AuthSpec `json:"auth,omitempty"`

	// +optional
	Logging LoggingSpec `json:"logging,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	Cluster ClusterTuningSpec `json:"cluster,omitempty"`

	// +optional
	Monitoring MonitoringSpec `json:"monitoring,omitempty"`

	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`

	// +optional
	Failover *FailoverSpec `json:"failover,omitempty"`

	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +optional
	AdditionalVolumes []corev1.Volume `json:"additionalVolumes,omitempty"`

	// +optional
	AdditionalVolumeMounts []corev1.VolumeMount `json:"additionalVolumeMounts,omitempty"`
}

type ServerSpec struct {
	// +kubebuilder:default=8086
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// +kubebuilder:default=26214400
	MaxBodySizeBytes int64 `json:"maxBodySizeBytes,omitempty"`

	// +kubebuilder:default=30
	RequestTimeoutSecs int32 `json:"requestTimeoutSecs,omitempty"`

	// +kubebuilder:default=30
	QueryTimeoutSecs int32 `json:"queryTimeoutSecs,omitempty"`

	// +optional
	TLS *TLSSpec `json:"tls,omitempty"`
}

type TLSSpec struct {
	Enabled bool `json:"enabled"`

	// Name of a Secret of type kubernetes.io/tls. When empty the operator
	// generates a self-signed CA and per-node certs.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// +optional
	CertManagerIssuerRef *CertManagerIssuerRef `json:"certManagerIssuerRef,omitempty"`
}

// CertManagerIssuerRef references a cert-manager Issuer or ClusterIssuer.
type CertManagerIssuerRef struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// +optional
	Group string `json:"group,omitempty"`
}

type StorageSpec struct {
	// +kubebuilder:default="local"
	// +kubebuilder:validation:Enum=local;s3
	Backend string `json:"backend,omitempty"`

	// +optional
	VolumeClaimTemplate *PersistentVolumeClaimSpec `json:"volumeClaimTemplate,omitempty"`

	// +optional
	S3 *S3StorageSpec `json:"s3,omitempty"`
}

type PersistentVolumeClaimSpec struct {
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`

	// +kubebuilder:default="10Gi"
	Size resource.Quantity `json:"size,omitempty"`
}

type S3StorageSpec struct {
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix,omitempty"`
	Region   string `json:"region,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`

	// Reference to a Secret containing access_key_id and secret_access_key.
	CredentialsSecretName string `json:"credentialsSecretName,omitempty"`
}

type FlushSpec struct {
	// +kubebuilder:default=10
	IntervalSecs int32 `json:"intervalSecs,omitempty"`

	// +kubebuilder:default=64
	WALSizeThresholdMB int32 `json:"walSizeThresholdMb,omitempty"`

	// +kubebuilder:default="1h"
	TimeBucketDuration string `json:"timeBucketDuration,omitempty"`
}

type CompactionSpec struct {
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`

	// +kubebuilder:default=300
	IntervalSecs int32 `json:"intervalSecs,omitempty"`

	// +kubebuilder:default=4
	MinFilesToCompact int32 `json:"minFilesToCompact,omitempty"`

	// +kubebuilder:default=256
	TargetFileSizeMB int32 `json:"targetFileSizeMb,omitempty"`
}

type ChDBSpec struct {
	// +kubebuilder:default=4
	PoolSize int32 `json:"poolSize,omitempty"`
}

type AuthSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled,omitempty"`

	// Reference to a Secret containing auth credentials.
	// +optional
	CredentialsSecretName string `json:"credentialsSecretName,omitempty"`
}

type LoggingSpec struct {
	// +kubebuilder:default="info"
	// +kubebuilder:validation:Enum=trace;debug;info;warn;error
	Level string `json:"level,omitempty"`

	// +kubebuilder:default="text"
	// +kubebuilder:validation:Enum=text;json
	Format string `json:"format,omitempty"`
}

type ClusterTuningSpec struct {
	// +kubebuilder:default=2
	HeartbeatIntervalSecs int32 `json:"heartbeatIntervalSecs,omitempty"`

	// +kubebuilder:default=5
	HeartbeatMissThreshold int32 `json:"heartbeatMissThreshold,omitempty"`

	// +kubebuilder:default=60
	AntiEntropyIntervalSecs int32 `json:"antiEntropyIntervalSecs,omitempty"`

	// When false, chinflux does not run periodic Merkle verify / delta sync.
	// +kubebuilder:default=true
	// +optional
	AntiEntropyEnabled *bool `json:"antiEntropyEnabled,omitempty"`

	// +kubebuilder:default=4
	SyncMaxConcurrentFiles int32 `json:"syncMaxConcurrentFiles,omitempty"`

	// +kubebuilder:default=5
	ReplicationMaxRetries int32 `json:"replicationMaxRetries,omitempty"`

	// +kubebuilder:default=300
	RaftHeartbeatIntervalMs int32 `json:"raftHeartbeatIntervalMs,omitempty"`

	// +kubebuilder:default=1000
	RaftElectionTimeoutMs int32 `json:"raftElectionTimeoutMs,omitempty"`

	// +kubebuilder:default=1000
	RaftSnapshotThreshold int32 `json:"raftSnapshotThreshold,omitempty"`

	// TLS for inter-node replication traffic.
	// +optional
	TLS *TLSSpec `json:"tls,omitempty"`
}

type MonitoringSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled,omitempty"`

	// +kubebuilder:default=true
	ServiceMonitor bool `json:"serviceMonitor,omitempty"`

	// Provision a Grafana dashboard ConfigMap with the grafana_dashboard label.
	// +optional
	GrafanaDashboard bool `json:"grafanaDashboard,omitempty"`
}

type AutoscalingSpec struct {
	Enabled bool `json:"enabled"`

	// +kubebuilder:validation:Minimum=1
	MinReplicas int32 `json:"minReplicas,omitempty"`

	// +kubebuilder:validation:Minimum=1
	MaxReplicas int32 `json:"maxReplicas"`

	// +kubebuilder:default=80
	TargetCPUUtilizationPercentage int32 `json:"targetCPUUtilizationPercentage,omitempty"`
}

// FailoverSpec controls automatic failure detection and recovery.
type FailoverSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// Maximum number of simultaneous failovers.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	MaxFailoverCount int32 `json:"maxFailoverCount,omitempty"`

	// Seconds a member must be unhealthy before triggering failover.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=60
	FailoverTimeoutSecs int32 `json:"failoverTimeoutSecs,omitempty"`
}

// ClusterPhase represents the lifecycle phase of the cluster.
// +kubebuilder:validation:Enum=Pending;Initializing;Running;Scaling;Upgrading;Failed
type ClusterPhase string

const (
	ClusterPhasePending      ClusterPhase = "Pending"
	ClusterPhaseInitializing ClusterPhase = "Initializing"
	ClusterPhaseRunning      ClusterPhase = "Running"
	ClusterPhaseScaling      ClusterPhase = "Scaling"
	ClusterPhaseUpgrading    ClusterPhase = "Upgrading"
	ClusterPhaseFailed       ClusterPhase = "Failed"
)

// MemberStatus describes the observed state of a single cluster member.
type MemberStatus struct {
	// Stable identifier derived from the StatefulSet ordinal.
	Name string `json:"name"`

	// Chinflux node_id (ordinal + 1).
	NodeID int32 `json:"nodeId"`

	// Kubernetes Pod name.
	PodName string `json:"podName"`

	// State reported by the node's /health endpoint (Active, Syncing, Joining, etc.).
	State string `json:"state"`

	// True when the pod passes its readiness probe.
	Health bool `json:"health"`

	// Last WAL sequence number on this node.
	// +optional
	WALSequence int64 `json:"walSequence,omitempty"`

	// Total number of parquet files on this node.
	// +optional
	ParquetFiles int32 `json:"parquetFiles,omitempty"`

	// Number of peers this node is aware of.
	// +optional
	PeerCount int32 `json:"peerCount,omitempty"`

	// Last time the member transitioned state.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
}

// ChinfluxClusterStatus defines the observed state of ChinfluxCluster.
type ChinfluxClusterStatus struct {
	// +kubebuilder:default="Pending"
	Phase ClusterPhase `json:"phase,omitempty"`

	Replicas      int32 `json:"replicas,omitempty"`
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// High-level cluster health: Healthy, Degraded, Recovering, Unknown.
	// +optional
	ClusterState string `json:"clusterState,omitempty"`

	// Replication convergence state: Healthy, Lagging, Diverged, Unknown.
	// +optional
	ReplicationState string `json:"replicationState,omitempty"`

	// Per-member status.
	// +optional
	Members []MemberStatus `json:"members,omitempty"`

	// Number of failovers executed in the current generation.
	// +optional
	FailoverCount int32 `json:"failoverCount,omitempty"`

	// Hash of the current config.toml used for rolling update detection.
	// +optional
	ConfigHash string `json:"configHash,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.status.clusterState`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ChinfluxCluster is the Schema for the chinfluxclusters API.
type ChinfluxCluster struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec ChinfluxClusterSpec `json:"spec"`

	// +optional
	Status ChinfluxClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChinfluxClusterList contains a list of ChinfluxCluster.
type ChinfluxClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChinfluxCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ChinfluxCluster{}, &ChinfluxClusterList{})
}
