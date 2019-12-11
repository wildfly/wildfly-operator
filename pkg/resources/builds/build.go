package builds

import (
	buildv1 "github.com/openshift/api/build/v1"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("wildlfyserver_statefulsets")

// GetOrCreateNewBuildConfig either returns the BuildConfig or create it
func GetOrCreateNewBuildConfig(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*buildv1.BuildConfig, error) {
	buildConfig := &buildv1.BuildConfig{}
	err := resources.Get(w, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, client, buildConfig)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	// create the buildConfig if it is not found
	if errors.IsNotFound(err) {
		buildConfig := newBuildConfig(w, labels)
		log.Info("Create BuildConfig", "Spec", buildConfig.Spec)

		if err := resources.Create(w, client, scheme, buildConfig); err != nil {
			if errors.IsAlreadyExists(err) {
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	}
	// buildConfig is found, update it if it does not match the wildlfyServer generation
	if !resources.IsCurrentGeneration(w, buildConfig) {
		newBuildConfig := newBuildConfig(w, labels)
		buildConfig.Labels = labels
		buildConfig.Spec = newBuildConfig.Spec

		if err := resources.Update(w, client, buildConfig); err != nil {
			if errors.IsInvalid(err) {
				// Can not update, so we delete to recreate the buildCOnfig from scratch
				if err := resources.Delete(w, client, buildConfig); err != nil {
					return nil, err
				}
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	}

	return buildConfig, nil
}

func newBuildConfig(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *buildv1.BuildConfig {

	incremental := true

	// use "openshift" namespace for the S2I builder image by default
	imageStreamNamespace := w.Spec.SourceRepository.ImageStreamNamespace
	if imageStreamNamespace == "" {
		imageStreamNamespace = "openshift"
	}

	buildConfig := &buildv1.BuildConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "build.openshift.io/v1",
			Kind:       "BuildConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.Name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				Source: buildv1.BuildSource{
					Type: "Git",
					Git: &buildv1.GitBuildSource{
						URI: w.Spec.SourceRepository.URL,
						Ref: w.Spec.SourceRepository.Ref,
					},
					ContextDir: w.Spec.SourceRepository.ContextDir,
				},
				Strategy: buildv1.BuildStrategy{
					Type: "Source",
					SourceStrategy: &buildv1.SourceBuildStrategy{
						Incremental: &incremental,
						ForcePull:   true,
						From: corev1.ObjectReference{
							Kind:      "ImageStreamTag",
							Namespace: imageStreamNamespace,
							Name:      resources.S2IBuilderImage,
						},
					},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{
						Kind: "ImageStreamTag",
						Name: w.Name + ":latest",
					},
				},
			},
			Triggers: []buildv1.BuildTriggerPolicy{{
				Type:        "ImageChange",
				ImageChange: &buildv1.ImageChangeTrigger{},
			}, {
				Type: "ConfigChange",
			}},
		},
	}

	if w.Spec.SourceRepository.GitHubWebHookSecret != "" {
		buildConfig.Spec.Triggers = append(buildConfig.Spec.Triggers, buildv1.BuildTriggerPolicy{
			Type: "GitHub",
			GitHubWebHook: &buildv1.WebHookTrigger{
				Secret: w.Spec.SourceRepository.GitHubWebHookSecret,
			}})
	}
	if w.Spec.SourceRepository.GenericWebHookSecret != "" {
		buildConfig.Spec.Triggers = append(buildConfig.Spec.Triggers, buildv1.BuildTriggerPolicy{
			Type: "Generic",
			GenericWebHook: &buildv1.WebHookTrigger{
				Secret: w.Spec.SourceRepository.GenericWebHookSecret,
			}})
	}

	if len(w.Spec.BuildEnv) > 0 {
		buildConfig.Spec.Strategy.SourceStrategy.Env = append(buildConfig.Spec.Strategy.SourceStrategy.Env, w.Spec.BuildEnv...)
	}

	return buildConfig
}
