package statefulsets

import (
	"os"

	"k8s.io/apimachinery/pkg/api/errors"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"

	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("wildlfyserver_statefulsets")

// GetOrCreateNewStatefulSet either returns the statefulset or create it
func GetOrCreateNewStatefulSet(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*appsv1.StatefulSet, error) {
	statefulSet := &appsv1.StatefulSet{}
	if err := resources.Get(w, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, client, statefulSet); err != nil {
		if errors.IsNotFound(err) {
			if err := resources.Create(w, client, scheme, NewStatefulSet(w, labels)); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	return statefulSet, nil
}

// NewStatefulSet retunrs a new statefulset
func NewStatefulSet(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *appsv1.StatefulSet {
	replicas := w.Spec.Size
	applicationImage := w.Spec.ApplicationImage
	volumeName := w.Name + "-volume"
	labesForActiveWildflyPod := labels
	labesForActiveWildflyPod[resources.MarkerOperatedByHeadless] = resources.MarkerServiceActive
	labesForActiveWildflyPod[resources.MarkerOperatedByLoadbalancer] = resources.MarkerServiceActive

	statefulSet := &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.Name,
			Namespace: w.Namespace,
			Labels:    labels,
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
					Labels: labesForActiveWildflyPod,
				},
				Spec: corev1.PodSpec{
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
						LivenessProbe: createLivenessProbe(),
						// Readiness Probe is options
						ReadinessProbe: createReadinessProbe(),
						VolumeMounts: []corev1.VolumeMount{{
							Name:      volumeName,
							MountPath: resources.StandaloneServerDataDirPath,
						}},
					}},
					ServiceAccountName: w.Spec.ServiceAccountName,
				},
			},
		},
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

	storageSpec := w.Spec.Storage

	if storageSpec == nil {
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if storageSpec.EmptyDir != nil {
		emptyDir := storageSpec.EmptyDir
		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: v1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	} else {
		pvcTemplate := storageSpec.VolumeClaimTemplate
		if pvcTemplate.Name == "" {
			pvcTemplate.Name = volumeName
		}
		pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		pvcTemplate.Spec.Resources = storageSpec.VolumeClaimTemplate.Spec.Resources
		pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
		statefulSet.Spec.VolumeClaimTemplates = append(statefulSet.Spec.VolumeClaimTemplates, pvcTemplate)
	}

	standaloneConfigMap := w.Spec.StandaloneConfigMap
	if standaloneConfigMap != nil {
		configMapName := standaloneConfigMap.Name
		configMapKey := standaloneConfigMap.Key
		if configMapKey == "" {
			configMapKey = "standalone.xml"
		}
		log.Info("Reading standalone configuration from configmap", "StandaloneConfigMap.Name", configMapName, "StandaloneConfigMap.Key", configMapKey)

		statefulSet.Spec.Template.Spec.Volumes = append(statefulSet.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "standalone-config-volume",
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: configMapName,
					},
					Items: []v1.KeyToPath{
						{
							Key:  configMapKey,
							Path: "standalone.xml",
						},
					},
				},
			},
		})
		statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(statefulSet.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "standalone-config-volume",
			MountPath: resources.JBossHome + "/standalone/configuration/standalone.xml",
			SubPath:   "standalone.xml",
		})
	}

	return statefulSet
}

// createLivenessProbe create a Exec probe if the SERVER_LIVENESS_SCRIPT env var is present.
// Otherwise, it creates a HTTPGet probe that checks the /health endpoint on the admin port.
//
// If defined, the SERVER_LIVENESS_SCRIPT env var must be the path of a shell script that
// complies to the Kuberenetes probes requirements.
func createLivenessProbe() *corev1.Probe {
	livenessProbeScript, defined := os.LookupEnv("SERVER_LIVENESS_SCRIPT")
	if defined {
		return &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &v1.ExecAction{
					Command: []string{"/bin/bash", "-c", livenessProbeScript},
				},
			},
			InitialDelaySeconds: 60,
		}
	}
	return &corev1.Probe{
		Handler: corev1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Path: "/health",
				Port: intstr.FromString("admin"),
			},
		},
		InitialDelaySeconds: 60,
	}
}

// createReadinessProbe create a Exec probe if the SERVER_READINESS_SCRIPT env var is present.
// Otherwise, it returns nil (i.e. no readiness probe is configured).
//
// If defined, the SERVER_READINESS_SCRIPT env var must be the path of a shell script that
// complies to the Kuberenetes probes requirements.
func createReadinessProbe() *corev1.Probe {
	readinessProbeScript, defined := os.LookupEnv("SERVER_READINESS_SCRIPT")
	if defined {
		return &corev1.Probe{
			Handler: corev1.Handler{
				Exec: &v1.ExecAction{
					Command: []string{"/bin/bash", "-c", readinessProbeScript},
				},
			},
		}
	}
	return nil
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
