package statefulsets

import (
	"encoding/json"
	"os"
	"path"
	"strconv"

	"k8s.io/apimachinery/pkg/api/errors"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/api/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"
	wildflyutil "github.com/wildfly/wildfly-operator/pkg/util"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("wildflyserver_statefulsets")

// GetOrCreateNewStatefulSet either returns the statefulset or create it
func GetOrCreateNewStatefulSet(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string, desiredReplicaSize int32, isOpenShift bool) (*appsv1.StatefulSet, error) {
	statefulSet := &appsv1.StatefulSet{}
	if err := resources.Get(w, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, client, statefulSet); err != nil {
		if errors.IsNotFound(err) {
			statefulSet = NewStatefulSet(w, labels, desiredReplicaSize, isOpenShift)
			if err := resources.Create(w, client, scheme, statefulSet); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	return statefulSet, nil
}

// NewStatefulSet returns a new statefulset
func NewStatefulSet(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, desiredReplicaSize int32, isOpenShift bool) *appsv1.StatefulSet {
	replicas := desiredReplicaSize
	applicationImage := w.Spec.ApplicationImage

	labelsForActiveWildflyPod := wildflyutil.CopyMap(labels)
	labelsForActiveWildflyPod[resources.MarkerOperatedByHeadless] = resources.MarkerServiceActive
	labelsForActiveWildflyPod[resources.MarkerOperatedByLoadbalancer] = resources.MarkerServiceActive
	applyLabels(resources.StatefuleSetTemplateLabelsEnvVarName, labelsForActiveWildflyPod)

	wildflyImageTypeAnnotation := resources.ImageTypeGeneric
	if w.Spec.BootableJar {
		wildflyImageTypeAnnotation = resources.ImageTypeBootable
	}

	podAnnotations := make(map[string]string)
	podAnnotations[resources.MarkerImageType] = wildflyImageTypeAnnotation
	if isOpenShift {
		podAnnotations["alpha.image.policy.openshift.io/resolve-names"] = "*"
	}

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.Name,
			Namespace: w.Namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"image.openshift.io/triggers": "[{ \"from\": { \"kind\":\"ImageStreamTag\", \"name\":\"" + w.Spec.ApplicationImage + "\"}, \"fieldPath\": \"spec.template.spec.containers[?(@.name==\\\"" + w.Name + "\\\")].image\"}]",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:            &replicas,
			ServiceName:         services.HeadlessServiceName(w),
			PodManagementPolicy: appsv1.ParallelPodManagement,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labelsForActiveWildflyPod,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{{
						Name:  w.Name,
						Image: applicationImage,
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: resources.HTTPApplicationPort,
								Name:          "http",
							},
							{
								ContainerPort: resources.HTTPManagementPort,
								Name:          "admin",
							},
						},
						LivenessProbe: createLivenessProbe(w),
						// Readiness Probe is optional
						ReadinessProbe: createReadinessProbe(w),
						// StartupProbe Probe is optional
						StartupProbe: createStartupProbe(w),
						// Resources
						Resources: createResources(w.Spec.Resources),
						// SecurityContext
						SecurityContext: w.Spec.SecurityContext,
					}},
					ServiceAccountName: w.Spec.ServiceAccountName,
				},
			},
		},
	}

	// if the user specified the resources directive propagate it to the container (required for HPA).
	if w.Spec.Resources != nil {
		statefulSet.Spec.Template.Spec.Containers[0].Resources = *w.Spec.Resources
	}

	// if the user specified the securityContext directive propagate it to the container (required for HPA).
	if w.Spec.SecurityContext != nil {
		statefulSet.Spec.Template.Spec.Containers[0].SecurityContext = *&w.Spec.SecurityContext
	}

	if len(w.Spec.EnvFrom) > 0 {
		statefulSet.Spec.Template.Spec.Containers[0].EnvFrom = append(statefulSet.Spec.Template.Spec.Containers[0].EnvFrom, w.Spec.EnvFrom...)
	}

	if len(w.Spec.Env) > 0 {
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, w.Spec.Env...)
	}

	// TODO the KUBERNETES_NAMESPACE and KUBERNETES_LABELS env should only be set if
	// the application uses clustering and KUBE_PING.
	statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, envForClustering(k8slabels.SelectorFromSet(labels).String())...)

	// the setup for the ejb remoting works fine the client binding is needed to be setup with the stateless headless service which is done in s2i
	statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, envForEJBRecovery(w)...)

	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{}

	storageSpec := w.Spec.Storage
	standaloneDataVolumeName := w.Name + "-volume"

	if storageSpec == nil {
		volumes = append(volumes, corev1.Volume{
			Name: standaloneDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if storageSpec.EmptyDir != nil {
		emptyDir := storageSpec.EmptyDir
		volumes = append(volumes, corev1.Volume{
			Name: standaloneDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	} else {
		pvcTemplate := storageSpec.VolumeClaimTemplate
		if pvcTemplate.Name == "" {
			pvcTemplate.Name = standaloneDataVolumeName
		}
		pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		pvcTemplate.Spec.Resources = createResources(&storageSpec.VolumeClaimTemplate.Spec.Resources)
		pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
		statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, pvcTemplate)
	}

	// mount the volume for the server standalone data directory
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      standaloneDataVolumeName,
		MountPath: path.Join(resources.JBossHomeDataDir(w.Spec.BootableJar), resources.StandaloneServerDataDirRelativePath),
	})

	// mount the volume to read the standalone XML configuration from a ConfigMap
	standaloneConfigMap := w.Spec.StandaloneConfigMap
	if standaloneConfigMap != nil {
		configMapName := standaloneConfigMap.Name
		configMapKey := standaloneConfigMap.Key
		if configMapKey == "" {
			configMapKey = "standalone.xml"
		}
		log.Info("Reading standalone configuration from configmap", "StandaloneConfigMap.Name", configMapName, "StandaloneConfigMap.Key", configMapKey)

		volumes = append(volumes, corev1.Volume{
			Name: "standalone-config-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: configMapName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  configMapKey,
							Path: "standalone.xml",
						},
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "standalone-config-volume",
			MountPath: path.Join(resources.JBossHome(w.Spec.BootableJar), "standalone/configuration/standalone.xml"),
			SubPath:   "standalone.xml",
		})
	}

	// mount volumes from secrets
	for _, s := range w.Spec.Secrets {
		volumeName := wildflyutil.SanitizeVolumeName("secret-" + s)
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			ReadOnly:  true,
			MountPath: path.Join(resources.SecretsDir, s),
		})
	}

	// mount volumes from config maps
	for _, cm := range w.Spec.ConfigMaps {
		volumeName := wildflyutil.SanitizeVolumeName("configmap-" + cm)
		volumes = append(volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cm,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      volumeName,
			ReadOnly:  true,
			MountPath: path.Join(resources.ConfigMapsDir, cm),
		})
	}

	statefulSet.Spec.Template.Spec.Volumes = volumes
	statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = volumeMounts

	// Configures the Bootable JAR environment
	if w.Spec.BootableJar {
		// Force Bootable JAR to be unzipped in a known directory
		statefulSet.Spec.Template.Spec.Containers[0].Env = append(statefulSet.Spec.Template.Spec.Containers[0].Env, envArgsForBootableJAR(resources.StandaloneServerDataDirRelativePath)...)
	}

	return statefulSet
}

// createResources supplements a default ResourceRequirements and returns it.
func createResources(r *corev1.ResourceRequirements) corev1.ResourceRequirements {
	rTemplate := corev1.ResourceRequirements{
		Limits:   nil,
		Requests: nil,
	}

	if r != nil {
		if r.Limits != nil && len(r.Limits) > 0 {
			rTemplate.Limits = r.Limits
		}

		if r.Requests != nil && len(r.Requests) > 0 {
			rTemplate.Requests = r.Requests
		}
	}

	return rTemplate
}

// createLivenessProbe create an Exec probe if the SERVER_LIVENESS_SCRIPT env var is present
// *and* the application is not using Bootable Jar *and* the liveness probe action has not been explicitly configured.
// Otherwise, it creates a HTTPGet probe that checks the /health/live endpoint on the admin port.
//
// If defined, the SERVER_LIVENESS_SCRIPT env var must be the path of a shell script that
// complies to the Kubernetes probes requirements.
// If SERVER_LIVENESS_SCRIPT script does not exist, then the Probe will execute the script defined by SERVER_LIVENESS_SCRIPT_FALLBACK
// If this script does not exist, the probe will execute and http get by using curl.
//
// If the liveness probe action, either Exec or HTTPGet, has been explicitly configured, then the Operator will create
// the action configured by the user. When both actions are configured at the same time, the Exec configuration will win
// and HTTPGet will be ignored.
func createLivenessProbe(w *wildflyv1alpha1.WildFlyServer) *corev1.Probe {
	probeScript, defined := os.LookupEnv("SERVER_LIVENESS_SCRIPT")

	var initialDelaySeconds int32 = 0
	var timeoutSeconds int32 = 1
	var periodSeconds int32 = 10
	var successThreshold int32 = 1
	var failureThreshold int32 = 3
	var probeHandler wildflyv1alpha1.ProbeHandler

	if w.Spec.LivenessProbe == nil {
		w.Spec.LivenessProbe = &wildflyv1alpha1.ProbeSpec{}
	}
	if w.Spec.LivenessProbe.InitialDelaySeconds != nil {
		initialDelaySeconds = *w.Spec.LivenessProbe.InitialDelaySeconds
	}
	if w.Spec.LivenessProbe.TimeoutSeconds != 0 {
		timeoutSeconds = w.Spec.LivenessProbe.TimeoutSeconds
	}
	if w.Spec.LivenessProbe.PeriodSeconds != 0 {
		periodSeconds = w.Spec.LivenessProbe.PeriodSeconds
	}
	if w.Spec.LivenessProbe.SuccessThreshold != 0 {
		successThreshold = w.Spec.LivenessProbe.SuccessThreshold
	}
	if w.Spec.LivenessProbe.FailureThreshold != 0 {
		failureThreshold = w.Spec.LivenessProbe.FailureThreshold
	}
	probeHandler = w.Spec.LivenessProbe.ProbeHandler

	return createCommonProbe(defined, w.Spec.BootableJar, probeScript, initialDelaySeconds, timeoutSeconds, periodSeconds, successThreshold, failureThreshold, probeHandler, "/health/live", "SERVER_LIVENESS_SCRIPT_FALLBACK")
}

// createReadinessProbe create an Exec probe if the SERVER_READINESS_SCRIPT env var is present
// *and* the application is not using Bootable Jar *and* the readiness probe action has not been explicitly configured.
//
// If defined, the SERVER_READINESS_SCRIPT env var must be the path of a shell script that
// complies to the Kubernetes probes requirements.
// If SERVER_READINESS_SCRIPT script does not exist, then the Probe will execute the script defined by SERVER_READINESS_SCRIPT_FALLBACK
// If this script does not exist, the probe will execute and http get by using curl.
//
// If the readiness probe action, either Exec or HTTPGet, has been explicitly configured, then the Operator will create
// the action configured by the user. When both actions are configured at the same time, the Exec configuration will win
// and HTTPGet will be ignored.
func createReadinessProbe(w *wildflyv1alpha1.WildFlyServer) *corev1.Probe {
	probeScript, defined := os.LookupEnv("SERVER_READINESS_SCRIPT")

	var initialDelaySeconds int32 = 10
	var timeoutSeconds int32 = 1
	var periodSeconds int32 = 10
	var successThreshold int32 = 1
	var failureThreshold int32 = 3
	var probeHandler wildflyv1alpha1.ProbeHandler

	if w.Spec.ReadinessProbe == nil {
		w.Spec.ReadinessProbe = &wildflyv1alpha1.ProbeSpec{}
	}
	if w.Spec.ReadinessProbe.InitialDelaySeconds != nil {
		initialDelaySeconds = *w.Spec.ReadinessProbe.InitialDelaySeconds
	}
	if w.Spec.ReadinessProbe.TimeoutSeconds != 0 {
		timeoutSeconds = w.Spec.ReadinessProbe.TimeoutSeconds
	}
	if w.Spec.ReadinessProbe.PeriodSeconds != 0 {
		periodSeconds = w.Spec.ReadinessProbe.PeriodSeconds
	}
	if w.Spec.ReadinessProbe.SuccessThreshold != 0 {
		successThreshold = w.Spec.ReadinessProbe.SuccessThreshold
	}
	if w.Spec.ReadinessProbe.FailureThreshold != 0 {
		failureThreshold = w.Spec.ReadinessProbe.FailureThreshold
	}
	probeHandler = w.Spec.ReadinessProbe.ProbeHandler

	return createCommonProbe(defined, w.Spec.BootableJar, probeScript, initialDelaySeconds, timeoutSeconds, periodSeconds, successThreshold, failureThreshold, probeHandler, "/health/ready", "SERVER_READINESS_SCRIPT_FALLBACK")
}

// createStartupProbe create an Exec probe if the SERVER_LIVENESS_SCRIPT env var is present
// *and* the application is not using Bootable Jar *and* the startup probe action has not been explicitly configured.
// Otherwise, it creates a HTTPGet probe that checks the /health/started endpoint on the admin port.
//
// If defined, the SERVER_LIVENESS_SCRIPT env var must be the path of a shell script that
// complies to the Kubernetes probes requirements.
// If SERVER_LIVENESS_SCRIPT script does not exist, then the Probe will execute the script defined by SERVER_LIVENESS_SCRIPT_FALLBACK
// If this script does not exist, the probe will execute and http get by using curl.
//
// If the startup probe action, either Exec or HTTPGet, has been explicitly configured, then the Operator will create
// the action configured by the user. When both actions are configured at the same time, the Exec configuration will win
// and HTTPGet will be ignored.
func createStartupProbe(w *wildflyv1alpha1.WildFlyServer) *corev1.Probe {
	probeScript, defined := os.LookupEnv("SERVER_LIVENESS_SCRIPT")

	var initialDelaySeconds int32 = 10
	var timeoutSeconds int32 = 1
	var periodSeconds int32 = 10
	var successThreshold int32 = 1
	var failureThreshold int32 = 11
	var probeHandler wildflyv1alpha1.ProbeHandler

	if w.Spec.StartupProbe == nil {
		w.Spec.StartupProbe = &wildflyv1alpha1.ProbeSpec{}
	}
	if w.Spec.StartupProbe.InitialDelaySeconds != nil {
		initialDelaySeconds = *w.Spec.StartupProbe.InitialDelaySeconds
	}
	if w.Spec.StartupProbe.TimeoutSeconds != 0 {
		timeoutSeconds = w.Spec.StartupProbe.TimeoutSeconds
	}
	if w.Spec.StartupProbe.PeriodSeconds != 0 {
		periodSeconds = w.Spec.StartupProbe.PeriodSeconds
	}
	if w.Spec.StartupProbe.SuccessThreshold != 0 {
		successThreshold = w.Spec.StartupProbe.SuccessThreshold
	}
	if w.Spec.StartupProbe.FailureThreshold != 0 {
		failureThreshold = w.Spec.StartupProbe.FailureThreshold
	}
	probeHandler = w.Spec.StartupProbe.ProbeHandler

	return createCommonProbe(defined, w.Spec.BootableJar, probeScript, initialDelaySeconds, timeoutSeconds, periodSeconds, successThreshold, failureThreshold, probeHandler, "/health/live", "SERVER_LIVENESS_SCRIPT_FALLBACK")
}

// creates the common parts for the Probes
func createCommonProbe(scriptDefined, bootableJar bool, probeScript string, initialDelaySeconds, timeoutSeconds int32, periodSeconds int32, successThreshold int32, failureThreshold int32, probeHandler wildflyv1alpha1.ProbeHandler, endPoint string, fallbackEnvProbe string) *corev1.Probe {
	var action corev1.ProbeHandler

	if probeHandler.Exec == nil && probeHandler.HTTPGet == nil {
		if scriptDefined && !bootableJar {
			livenessProbeScriptFallback, definedFallback := os.LookupEnv(fallbackEnvProbe)
			if !definedFallback {
				livenessProbeScriptFallback = "curl --fail http://127.0.0.1:" + strconv.Itoa(int(resources.HTTPManagementPort)) + endPoint
			}
			probeScript := "if [ -f '" + probeScript + "' ]; then " + probeScript + "; else " + livenessProbeScriptFallback + "; fi"
			action.Exec = &corev1.ExecAction{
				Command: []string{"/bin/bash", "-c", probeScript},
			}
		} else {
			action.HTTPGet = &corev1.HTTPGetAction{
				Path: endPoint,
				Port: intstr.FromString("admin"),
			}
		}
	} else if probeHandler.Exec != nil {
		action.Exec = &corev1.ExecAction{
			Command: probeHandler.Exec.Command,
		}
	} else {
		action.HTTPGet = &corev1.HTTPGetAction{
			Path:        probeHandler.HTTPGet.Path,
			Port:        probeHandler.HTTPGet.Port,
			Host:        probeHandler.HTTPGet.Host,
			Scheme:      probeHandler.HTTPGet.Scheme,
			HTTPHeaders: probeHandler.HTTPGet.HTTPHeaders,
		}
	}

	log.Info("Creating probe",
		"initialDelaySeconds", initialDelaySeconds,
		"timeoutSeconds", timeoutSeconds,
		"periodSeconds", periodSeconds,
		"successThreshold", successThreshold,
		"failureThreshold", failureThreshold)

	return &corev1.Probe{
		ProbeHandler:        action,
		InitialDelaySeconds: initialDelaySeconds,
		TimeoutSeconds:      timeoutSeconds,
		PeriodSeconds:       periodSeconds,
		SuccessThreshold:    successThreshold,
		FailureThreshold:    failureThreshold,
	}
}

func envForClustering(labels string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "KUBERNETES_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.namespace",
				},
			},
		},
		{
			Name:  "KUBERNETES_LABELS",
			Value: labels,
		},
	}
}

func envForEJBRecovery(w *wildflyv1alpha1.WildFlyServer) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "STATEFULSET_HEADLESS_SERVICE_NAME",
			Value: services.HeadlessServiceName(w),
		},
	}
}

func envArgsForBootableJAR(defaultDataDir string) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "JAVA_ARGS",
			Value: "-Djboss.server.data.dir=" + path.Join(resources.JBossHomeDataDir(true), defaultDataDir) + " --install-dir=" + resources.JBossHome(true),
		},
		{
			Name:  "JBOSS_HOME",
			Value: resources.JBossHome(true),
		},
	}
}

func applyLabels(envvar string, labels map[string]string) {
	labelsFromEnv := os.Getenv(envvar)
	if labelsFromEnv == "" {
		return
	}
	var labelMap map[string]string
	if err := json.Unmarshal([]byte(labelsFromEnv), &labelMap); err != nil {
		return
	}
	if len(labelMap) > 0 {
		for name, value := range labelMap {
			labels[name] = value
		}
	}
}
