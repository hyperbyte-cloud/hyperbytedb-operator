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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChinfluxBackupSpec defines the desired state of ChinfluxBackup.
type ChinfluxBackupSpec struct {
	// +kubebuilder:validation:MinLength=1
	ClusterName string `json:"clusterName"`

	// Cron expression for scheduled backups. If empty, the backup is one-shot.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// +required
	Destination BackupDestination `json:"destination"`

	// +kubebuilder:default=7
	// +kubebuilder:validation:Minimum=1
	RetentionDays int32 `json:"retentionDays,omitempty"`

	// Restrict the backup to specific databases. Empty means all databases.
	// +optional
	Databases []string `json:"databases,omitempty"`

	// +kubebuilder:default="full"
	// +kubebuilder:validation:Enum=full;incremental
	BackupType string `json:"backupType,omitempty"`
}

type BackupDestination struct {
	S3 S3BackupSpec `json:"s3"`
}

type S3BackupSpec struct {
	Bucket   string `json:"bucket"`
	Prefix   string `json:"prefix,omitempty"`
	Region   string `json:"region,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`

	// Reference to a Secret containing access_key_id and secret_access_key.
	CredentialsSecretName string `json:"credentialsSecretName,omitempty"`
}

// BackupPhase represents the lifecycle phase of a backup.
// +kubebuilder:validation:Enum=Pending;Running;Completed;Failed
type BackupPhase string

const (
	BackupPhasePending   BackupPhase = "Pending"
	BackupPhaseRunning   BackupPhase = "Running"
	BackupPhaseCompleted BackupPhase = "Completed"
	BackupPhaseFailed    BackupPhase = "Failed"
)

// ChinfluxBackupStatus defines the observed state of ChinfluxBackup.
type ChinfluxBackupStatus struct {
	// +kubebuilder:default="Pending"
	Phase BackupPhase `json:"phase,omitempty"`

	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Human-readable backup size (e.g. "1.2 GiB").
	// +optional
	BackupSize string `json:"backupSize,omitempty"`

	// S3 path where the backup was stored.
	// +optional
	BackupPath string `json:"backupPath,omitempty"`

	// +optional
	LastCleanupTime *metav1.Time `json:"lastCleanupTime,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=`.spec.schedule`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.status.backupSize`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ChinfluxBackup is the Schema for the chinfluxbackups API.
type ChinfluxBackup struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec ChinfluxBackupSpec `json:"spec"`

	// +optional
	Status ChinfluxBackupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChinfluxBackupList contains a list of ChinfluxBackup.
type ChinfluxBackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChinfluxBackup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ChinfluxBackup{}, &ChinfluxBackupList{})
}
