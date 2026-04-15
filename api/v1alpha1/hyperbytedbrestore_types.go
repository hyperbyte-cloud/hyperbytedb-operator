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

// HyperbytedbRestoreSpec defines the desired state of HyperbytedbRestore.
type HyperbytedbRestoreSpec struct {
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
// +kubebuilder:validation:Enum=Pending;ScalingDown;Restoring;Completed;Failed
type RestorePhase string

const (
	RestorePhasePending   RestorePhase = "Pending"
	RestorePhaseScaleDown RestorePhase = "ScalingDown"
	RestorePhaseRestoring RestorePhase = "Restoring"
	RestorePhaseCompleted RestorePhase = "Completed"
	RestorePhaseFailed    RestorePhase = "Failed"
)

// HyperbytedbRestoreStatus defines the observed state of HyperbytedbRestore.
type HyperbytedbRestoreStatus struct {
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

// HyperbytedbRestore is the Schema for the hyperbytedbrestores API.
type HyperbytedbRestore struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec HyperbytedbRestoreSpec `json:"spec"`

	// +optional
	Status HyperbytedbRestoreStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// HyperbytedbRestoreList contains a list of HyperbytedbRestore.
type HyperbytedbRestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []HyperbytedbRestore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HyperbytedbRestore{}, &HyperbytedbRestoreList{})
}
