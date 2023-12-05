/*
Copyright 2022.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// WildFlyServerSpec defines the desired state of WildFlyServer
// +k8s:openapi-gen=true
type WildFlyServerSpec struct {
	// ApplicationImage is the name of the application image to be deployed
	ApplicationImage string `json:"applicationImage"`
	// BootableJar specifies whether the application image is using S2I Builder/Runtime images or Bootable Jar.
	// If omitted, it defaults to false (application image is expected to use S2I Builder/Runtime images)
	BootableJar bool `json:"bootableJar,omitempty"`
	// Replicas is the desired number of replicas for the application
	// +kubebuilder:validation:Minimum=0
	Replicas int32 `json:"replicas"`
	// SessionAffinity defines if connections from the same client ip are passed to the same WildFlyServer instance/pod each time (false if omitted)
	SessionAffinity bool `json:"sessionAffinity,omitempty"`
	// DisableHTTPRoute disables the creation a route to the HTTP port of the application service (false if omitted)
	DisableHTTPRoute    bool                     `json:"disableHTTPRoute,omitempty"`
	StandaloneConfigMap *StandaloneConfigMapSpec `json:"standaloneConfigMap,omitempty"`
	// StorageSpec defines specific storage required for the server own data directory. If omitted, an EmptyDir is used (that will not
	// persist data across pod restart).
	Storage            *StorageSpec `json:"storage,omitempty"`
	ServiceAccountName string       `json:"serviceAccountName,omitempty"`
	// EnvFrom contains environment variables from a source such as a ConfigMap or a Secret
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty,list_type=corev1.EnvFromSource"`
	// Env contains environment variables for the containers running the WildFlyServer application
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Secrets is a list of Secrets in the same namespace as the WildFlyServer
	// object, which shall be mounted into the WildFlyServer Pods.
	// The Secrets are mounted into /etc/secrets/<secret-name>.
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	Secrets []string `json:"secrets,omitempty"`
	// ConfigMaps is a list of ConfigMaps in the same namespace as the WildFlyServer
	// object, which shall be mounted into the WildFlyServer Pods.
	// The ConfigMaps are mounted into /etc/configmaps/<configmap-name>.
	// +kubebuilder:validation:MinItems=1
	// +listType=set
	ConfigMaps []string `json:"configMaps,omitempty"`
	// ResourcesSpec defines the resources used by the WildFlyServer, ie CPU and memory, use limits and requests.
	// More info: https://pkg.go.dev/k8s.io/api@v0.18.14/core/v1#ResourceRequirements
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// SecurityContext defines the security capabilities required to run the application.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
	// LivenessProbe defines the periodic probe of container liveness. Container will be restarted if the probe fails.
	LivenessProbe *ProbeSpec `json:"livenessProbe,omitempty"`
	// ReadinessProbe defines the periodic probe of container service readiness. Container will be removed from service endpoints if the probe fails.
	ReadinessProbe *ProbeSpec `json:"readinessProbe,omitempty"`
	// StartupProbe indicates that the Pod has successfully initialized. If specified, no other probes are executed until this completes successfully.
	// If this probe fails, the Pod will be restarted, just as if the livenessProbe failed. This can be used to provide different probe parameters at the beginning of a Pod's lifecycle,
	// when it might take a long time to load data or warm a cache, than during steady-state operation.
	StartupProbe *ProbeSpec `json:"startupProbe,omitempty"`
}

// ProbeSpec describes a health check to be performed against a container to determine whether it is alive or ready to receive traffic.
// The Operator will configure the exec/httpGet fields if they are not explicitly defined in the probe.
// +k8s:openapi-gen=true
type ProbeSpec struct {
	// The action taken to determine the health of a container
	ProbeHandler `json:",inline,omitempty" protobuf:"bytes,1,opt,name=handler"`
	// Number of seconds after the container has started before probes are initiated.
	// It defaults to 60 seconds for liveness probe. It defaults to 10 seconds for readiness probe. It defaults to 0 seconds for startup probe.
	// Minimum value is 0.
	// +kubebuilder:validation:Minimum=0
	// +optional
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// Number of seconds after which the probe times out.
	// Defaults to 1 second. Minimum value is 1.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +kubebuilder:validation:Minimum=1
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
	// How often (in seconds) to perform the probe.
	// Default to 10 seconds. Minimum value is 1.
	// +kubebuilder:validation:Minimum=1
	// +optional
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// Minimum consecutive successes for the probe to be considered successful after having failed.
	// Defaults to 1. Must be 1 for liveness and startup. Minimum value is 1.
	// +kubebuilder:validation:Minimum=1
	// +optional
	SuccessThreshold int32 `json:"successThreshold,omitempty"`
	// Minimum consecutive failures for the probe to be considered failed after having succeeded.
	// Defaults to 3. Minimum value is 1.
	// +kubebuilder:validation:Minimum=1
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// ProbeHandler defines a specific action between Exec or HTTPGet that should be taken in a probe.
// If Exec and HTTPGet handlers are both defined, the Operator will configure the Exec handler and will ignore the
// HTTPGet one.
type ProbeHandler struct {
	// Exec specifies a command action to take.
	// +optional
	Exec *corev1.ExecAction `json:"exec,omitempty" protobuf:"bytes,1,opt,name=exec"`
	// HTTPGet specifies the http request to perform.
	// +optional
	HTTPGet *corev1.HTTPGetAction `json:"httpGet,omitempty" protobuf:"bytes,2,opt,name=httpGet"`
}

// StandaloneConfigMapSpec defines the desired configMap configuration to obtain the standalone configuration for WildFlyServer
// +k8s:openapi-gen=true
type StandaloneConfigMapSpec struct {
	Name string `json:"name"`
	// Key of the config map whose value is the standalone XML configuration file ("standalone.xml" if omitted)
	Key string `json:"key,omitempty"`
}

// StorageSpec defines the desired storage for WildFlyServer
// +k8s:openapi-gen=true
type StorageSpec struct {
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// VolumeClaimTemplate defines the template to store WildFlyServer standalone data directory.
	// The name of the template is derived from the WildFlyServer name.
	//  The corresponding volume will be mounted in ReadWriteOnce access mode.
	// This template should be used to specify specific Resources requirements in the template spec.
	VolumeClaimTemplate corev1.PersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// WildFlyServerStatus defines the observed state of WildFlyServer
// +k8s:openapi-gen=true
type WildFlyServerStatus struct {
	// Replicas is the actual number of replicas for the application
	Replicas int32 `json:"replicas"`
	// +listType=atomic
	Pods []PodStatus `json:"pods,omitempty"`
	// +listType=set
	Hosts []string `json:"hosts,omitempty"`
	// Represents the number of pods which are in scaledown process
	// what particular pod is scaling down can be verified by PodStatus
	//
	// Read-only.
	ScalingdownPods int32 `json:"scalingdownPods"`
	// selector for pods, used by HorizontalPodAutoscaler
	Selector string `json:"selector,omitempty"`
}

const (
	// PodStateActive represents PodStatus.State when pod is active to serve requests
	// it's connected in the Service load balancer
	PodStateActive = "ACTIVE"
	// PodStateScalingDownRecoveryInvestigation represents the PodStatus.State when pod is in state of scaling down
	// and is to be verified if it's dirty and if recovery is needed
	// as the pod is under recovery verification it can't be immediately removed
	// and it needs to be wait until it's marked as clean to be removed
	PodStateScalingDownRecoveryInvestigation = "SCALING_DOWN_RECOVERY_INVESTIGATION"
	// PodStateScalingDownRecoveryDirty represents the PodStatus.State when the pod was marked as recovery is needed
	// because there are some in-doubt transactions.
	// The app server was restarted with the recovery properties to speed-up recovery nad it's needed to wait
	// until all ind-doubt transactions are processed.
	PodStateScalingDownRecoveryDirty = "SCALING_DOWN_RECOVERY_DIRTY"
	// PodStateScalingDownClean represents the PodStatus.State when pod is not active to serve requests
	// it's in state of scaling down and it's clean
	// 'clean' means it's ready to be removed from the kubernetes cluster
	PodStateScalingDownClean = "SCALING_DOWN_CLEAN"
)

// PodStatus defines the observed state of pods running the WildFlyServer application
// +k8s:openapi-gen=true
type PodStatus struct {
	Name  string `json:"name"`
	PodIP string `json:"podIP"`
	// Represent the state of the Pod, it is used especially during scale down.
	// +kubebuilder:validation:Enum=ACTIVE;SCALING_DOWN_RECOVERY_INVESTIGATION;SCALING_DOWN_RECOVERY_DIRTY;SCALING_DOWN_CLEAN
	State string `json:"state"`
}

// WildFlyServer is the Schema for the wildflyservers API
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Replicas",type="integer",JSONPath=".spec.replicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:shortName=wfly
type WildFlyServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WildFlyServerSpec   `json:"spec,omitempty"`
	Status WildFlyServerStatus `json:"status,omitempty"`
}

// WildFlyServerList contains a list of WildFlyServer
// +kubebuilder:object:root=true
type WildFlyServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WildFlyServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WildFlyServer{}, &WildFlyServerList{})
}
