package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// WildFlyServerSpec defines the desired state of WildFlyServer
// +k8s:openapi-gen=true
type WildFlyServerSpec struct {
	// ApplicationImage is the name of the application image to be deployed
	ApplicationImage string       `json:"applicationImage"`
	Size             int32        `json:"size"`
	Storage          *StorageSpec `json:"storage,omitempty"`
}

// StorageSpec defines the desired storage for WildFlyServer
// +k8s:openapi-gen=true
type StorageSpec struct {
	EmptyDir            *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	VolumeClaimTemplate corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// WildFlyServerStatus defines the observed state of WildFlyServer
// +k8s:openapi-gen=true
type WildFlyServerStatus struct {
	Nodes []string `json:"nodes"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WildFlyServer is the Schema for the wildflyservers API
// +k8s:openapi-gen=true
type WildFlyServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WildFlyServerSpec   `json:"spec,omitempty"`
	Status WildFlyServerStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// WildFlyServerList contains a list of WildFlyServer
type WildFlyServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WildFlyServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WildFlyServer{}, &WildFlyServerList{})
}
