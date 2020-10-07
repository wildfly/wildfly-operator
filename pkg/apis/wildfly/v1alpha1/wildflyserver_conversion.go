package v1alpha1

import (
	"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("webhook")

// Converts from v1alpha1 to Hub
func (src *WildFlyServer) ConvertTo(dstRaw conversion.Hub) error {
	log.Info("Converting from v1alpha1 to v1alpha2")
	dst := dstRaw.(*v1alpha2.WildFlyServer)

	log.V(1).Info("Source", "Object:", dst)

	dst.Spec.ConfigMaps = make([]v1alpha2.ConfigMapSpec, len(src.Spec.ConfigMaps))
	for i, value := range src.Spec.ConfigMaps {
		dstCmSpec := v1alpha2.ConfigMapSpec{
			Name: value,
		}
		dst.Spec.ConfigMaps[i] = dstCmSpec
	}

	// ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.ApplicationImage = src.Spec.ApplicationImage
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.SessionAffinity = src.Spec.SessionAffinity
	dst.Spec.DisableHTTPRoute = src.Spec.DisableHTTPRoute
	if src.Spec.StandaloneConfigMap != nil {
		dst.Spec.StandaloneConfigMap = &v1alpha2.StandaloneConfigMapSpec{
			Name: src.Spec.StandaloneConfigMap.Name,
			Key:  src.Spec.StandaloneConfigMap.Key,
		}
	}
	if src.Spec.Storage != nil {
		// Notice we are copy a pointer here, we could use a deep copy instead ??
		dst.Spec.Storage = &v1alpha2.StorageSpec{
			EmptyDir:            src.Spec.Storage.EmptyDir,
			VolumeClaimTemplate: src.Spec.Storage.VolumeClaimTemplate,
		}
	}
	dst.Spec.ServiceAccountName = src.Spec.ServiceAccountName
	dst.Spec.EnvFrom = src.Spec.EnvFrom
	dst.Spec.Env = src.Spec.Env
	dst.Spec.Secrets = src.Spec.Secrets

	// Status
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.Pods = make([]v1alpha2.PodStatus, len(src.Status.Pods))
	for i, value := range src.Status.Pods {
		dstPodStatus := v1alpha2.PodStatus{
			Name:  value.Name,
			PodIP: value.PodIP,
			State: value.State,
		}
		dst.Status.Pods[i] = dstPodStatus
	}

	dst.Status.Hosts = src.Status.Hosts
	dst.Status.ScalingdownPods = src.Status.ScalingdownPods

	log.V(1).Info("Converted", "Object:", dst)

	return nil
}

// Converts from Hub to v1alpha1
func (dst *WildFlyServer) ConvertFrom(srcRaw conversion.Hub) error {
	log.Info("Converting from v1alpha2 to v1alpha1")

	src := srcRaw.(*v1alpha2.WildFlyServer)

	log.V(1).Info("Source", "Object:", dst)

	dst.Spec.ConfigMaps = make([]string, len(src.Spec.ConfigMaps))
	for i, value := range src.Spec.ConfigMaps {
		dst.Spec.ConfigMaps[i] = value.Name
	}

	// ObjectMeta
	dst.ObjectMeta = src.ObjectMeta

	// Spec
	dst.Spec.ApplicationImage = src.Spec.ApplicationImage
	dst.Spec.Replicas = src.Spec.Replicas
	dst.Spec.SessionAffinity = src.Spec.SessionAffinity
	dst.Spec.DisableHTTPRoute = src.Spec.DisableHTTPRoute
	if src.Spec.StandaloneConfigMap != nil {
		dst.Spec.StandaloneConfigMap = &StandaloneConfigMapSpec{
			Name: src.Spec.StandaloneConfigMap.Name,
			Key:  src.Spec.StandaloneConfigMap.Key,
		}
	}
	if src.Spec.Storage != nil {
		// Notice we are copy a pointer here, we could use a deep copy instead ??
		dst.Spec.Storage = &StorageSpec{
			EmptyDir:            src.Spec.Storage.EmptyDir,
			VolumeClaimTemplate: src.Spec.Storage.VolumeClaimTemplate,
		}
	}
	dst.Spec.ServiceAccountName = src.Spec.ServiceAccountName
	dst.Spec.EnvFrom = src.Spec.EnvFrom
	dst.Spec.Env = src.Spec.Env
	dst.Spec.Secrets = src.Spec.Secrets

	// Status
	dst.Status.Replicas = src.Status.Replicas
	dst.Status.Pods = make([]PodStatus, len(src.Status.Pods))
	for i, value := range src.Status.Pods {
		dstPodStatus := PodStatus{
			Name:  value.Name,
			PodIP: value.PodIP,
			State: value.State,
		}
		dst.Status.Pods[i] = dstPodStatus
	}
	dst.Status.Hosts = src.Status.Hosts
	dst.Status.ScalingdownPods = src.Status.ScalingdownPods

	log.V(1).Info("Converted", "Object:", dst)

	return nil
}
