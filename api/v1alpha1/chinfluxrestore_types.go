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

// ChinfluxRestoreSpec defines the desired state of ChinfluxRestore.
type ChinfluxRestoreSpec struct {
	// +kubebuilder:validation:MinLength=1
	ClusterName string `json:"clusterName"`

	// +kubebuilder:validation:MinLength=1
	BackupName string `json:"backupName"`

	// +optional
	Source *RestoreSource `json:"source,omitempty"`

	// Optional RFC-3339 timestamp for point-in-time restore (future use).
	// +optional
	RestoreTimestamp *metav1.Time `json:"restoreTimestamp,omitempty"`
}

type RestoreSource struct {
	S3 S3BackupSpec `json:"s3"`
}

// RestorePhase represents the lifecycle phase of a restore.
// +kubebuilder:validation:Enum=Pending;ScalingDown;Restoring;ScalingUp;Completed;Failed
type RestorePhase string

const (
	RestorePhasePending   RestorePhase = "Pending"
	RestorePhaseScaleDown RestorePhase = "ScalingDown"
	RestorePhaseRestoring RestorePhase = "Restoring"
	RestorePhaseScaleUp   RestorePhase = "ScalingUp"
	RestorePhaseCompleted RestorePhase = "Completed"
	RestorePhaseFailed    RestorePhase = "Failed"
)

// ChinfluxRestoreStatus defines the observed state of ChinfluxRestore.
type ChinfluxRestoreStatus struct {
	// +kubebuilder:default="Pending"
	Phase RestorePhase `json:"phase,omitempty"`

	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Number of PVCs restored (relevant in cluster mode).
	// +optional
	RestoredPVCs int32 `json:"restoredPVCs,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterName`
// +kubebuilder:printcolumn:name="Backup",type=string,JSONPath=`.spec.backupName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// ChinfluxRestore is the Schema for the chinfluxrestores API.
type ChinfluxRestore struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec ChinfluxRestoreSpec `json:"spec"`

	// +optional
	Status ChinfluxRestoreStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ChinfluxRestoreList contains a list of ChinfluxRestore.
type ChinfluxRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ChinfluxRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ChinfluxRestore{}, &ChinfluxRestoreList{})
}
