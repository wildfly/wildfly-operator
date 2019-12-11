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

var log = logf.Log.WithName("wildlfyserver_buildconfigs")

// GetOrCreateNewBuilderBuildConfig either returns the BuildConfig or create it
func GetOrCreateNewBuilderBuildConfig(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*buildv1.BuildConfig, error) {
	return getOrCreateNewBuildConfig(w, client, scheme, labels, newBuilderBuildConfig, w.Name)
}

// GetOrCreateNewRuntimeBuildConfig either returns the BuildConfig or create it
func GetOrCreateNewRuntimeBuildConfig(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*buildv1.BuildConfig, error) {
	return getOrCreateNewBuildConfig(w, client, scheme, labels, newRuntimeBuildConfig, w.Name+"-runtime")
}

func getOrCreateNewBuildConfig(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string, createBuildConfig func(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, name string) *buildv1.BuildConfig, name string) (*buildv1.BuildConfig, error) {
	buildConfig := &buildv1.BuildConfig{}
	err := resources.Get(w, types.NamespacedName{Name: name, Namespace: w.Namespace}, client, buildConfig)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	// create the buildConfig if it is not found
	if errors.IsNotFound(err) {
		buildConfig := createBuildConfig(w, labels, name)
		log.Info("Create BuildConfig", "Spec", buildConfig.Spec)

		if err := resources.Create(w, client, scheme, buildConfig); err != nil {
			if errors.IsAlreadyExists(err) {
				return nil, nil
			}
			return nil, err
		}
		return buildConfig, nil
	}
	// buildConfig is found, update it if it does not match the wildlfyServer generation
	if !resources.IsCurrentGeneration(w, buildConfig) {
		newBuildConfig := createBuildConfig(w, labels, name)
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

func newBuilderBuildConfig(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, name string) *buildv1.BuildConfig {

	incremental := true

	builderImage := w.Spec.ApplicationSource.Source2Image.BuilderImage
	// use "openshift" namespace for the S2I builder image by default
	builderImageNamespace := w.Spec.ApplicationSource.Source2Image.Namespace
	if builderImageNamespace == "" {
		builderImageNamespace = "openshift"
	}

	buildConfig := &buildv1.BuildConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "build.openshift.io/v1",
			Kind:       "BuildConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				Source: buildv1.BuildSource{
					Type: "Git",
					Git: &buildv1.GitBuildSource{
						URI: w.Spec.ApplicationSource.SourceRepository.URL,
						Ref: w.Spec.ApplicationSource.SourceRepository.Ref,
					},
					ContextDir: w.Spec.ApplicationSource.SourceRepository.ContextDir,
				},
				Strategy: buildv1.BuildStrategy{
					Type: "Source",
					SourceStrategy: &buildv1.SourceBuildStrategy{
						Incremental: &incremental,
						ForcePull:   true,
						From: corev1.ObjectReference{
							Kind:      "ImageStreamTag",
							Namespace: builderImageNamespace,
							Name:      builderImage,
						},
					},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{
						Kind: "ImageStreamTag",
						Name: name + ":latest",
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

	buildConfig.Spec.Triggers = append(buildConfig.Spec.Triggers, buildv1.BuildTriggerPolicy{
		Type: "GitHub",
		GitHubWebHook: &buildv1.WebHookTrigger{
			SecretReference: &buildv1.SecretLocalReference{
				Name: w.Spec.ApplicationSource.SourceRepository.GitHubWebHookSecret,
			},
		},
	})
	buildConfig.Spec.Triggers = append(buildConfig.Spec.Triggers, buildv1.BuildTriggerPolicy{
		Type: "Generic",
		GenericWebHook: &buildv1.WebHookTrigger{
			SecretReference: &buildv1.SecretLocalReference{
				Name: w.Spec.ApplicationSource.SourceRepository.GenericWebHookSecret,
			},
		},
	})

	if len(w.Spec.ApplicationSource.Source2Image.Env) > 0 {
		buildConfig.Spec.Strategy.SourceStrategy.Env = append(buildConfig.Spec.Strategy.SourceStrategy.Env, w.Spec.ApplicationSource.Source2Image.Env...)
	}

	if w.Spec.ApplicationSource.Source2Image.RuntimeImage != "" {
		// check if the GALLEON_PROVISION_LAYERS env var is defined. If it is not, GALLEON_PROVISION_DEFAULT_FAT_SERVER must be set to true
		found := false
		for _, env := range w.Spec.ApplicationSource.Source2Image.Env {
			if env.Name == "GALLEON_PROVISION_LAYERS" && env.Value != "" {
				found = true
				break
			}
		}
		if !found {
			buildConfig.Spec.Strategy.SourceStrategy.Env = append(buildConfig.Spec.Strategy.SourceStrategy.Env, corev1.EnvVar{
				Name:  "GALLEON_PROVISION_DEFAULT_FAT_SERVER",
				Value: "true",
			})
		}
	}

	return buildConfig
}

func newRuntimeBuildConfig(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, name string) *buildv1.BuildConfig {

	builderImage := w.Spec.ApplicationSource.Source2Image.BuilderImage
	runtimeImage := w.Spec.ApplicationSource.Source2Image.RuntimeImage
	// use "openshift" namespace for the S2I runtime image by default, otherwise use the same namespace than the builder image
	runtimeImageNamespace := w.Spec.ApplicationSource.Source2Image.Namespace
	if runtimeImageNamespace == "" {
		runtimeImageNamespace = "openshift"
	}

	dockerFile := "FROM " + runtimeImage + "\n" +
		"COPY /server $JBOSS_HOME\n" +
		"USER root\n" +
		"RUN chown -R jboss:root $JBOSS_HOME && chmod -R ug+rwX $JBOSS_HOME\n" +
		"RUN ln -s $JBOSS_HOME /wildfly\n" +
		"USER jboss\n" +
		"CMD $JBOSS_HOME/bin/openshift-launch.sh"
	skipLayers := buildv1.ImageOptimizationSkipLayers

	buildConfig := &buildv1.BuildConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "build.openshift.io/v1",
			Kind:       "BuildConfig",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: buildv1.BuildConfigSpec{
			CommonSpec: buildv1.CommonSpec{
				Source: buildv1.BuildSource{
					Type:       "Dockerfile",
					Dockerfile: &dockerFile,
					Images: []buildv1.ImageSource{
						{
							From: corev1.ObjectReference{
								Kind: "ImageStreamTag",
								Name: w.Name + ":latest",
							},
							Paths: []buildv1.ImageSourcePath{
								{
									SourcePath:     "/s2i-output/server/",
									DestinationDir: ".",
								},
							},
						},
					},
				},
				Strategy: buildv1.BuildStrategy{
					Type: "Docker",
					DockerStrategy: &buildv1.DockerBuildStrategy{
						ImageOptimizationPolicy: &skipLayers,
						From: &corev1.ObjectReference{
							Kind:      "ImageStreamTag",
							Namespace: runtimeImageNamespace,
							Name:      runtimeImage,
						},
					},
				},
				Output: buildv1.BuildOutput{
					To: &corev1.ObjectReference{
						Kind: "ImageStreamTag",
						Name: name + ":latest",
					},
				},
			},
			Triggers: []buildv1.BuildTriggerPolicy{
				{
					Type: "ImageChange",
					ImageChange: &buildv1.ImageChangeTrigger{
						From: &corev1.ObjectReference{
							Kind: "ImageStreamTag",
							Name: builderImage,
						}}},
				{
					Type: "ImageChange",
					ImageChange: &buildv1.ImageChangeTrigger{
						From: &corev1.ObjectReference{
							Kind: "ImageStreamTag",
							Name: w.Name + ":latest",
						}}},
				{
					Type: "ConfigChange",
				},
			},
		},
	}

	if len(w.Spec.ApplicationSource.Source2Image.Env) > 0 {
		buildConfig.Spec.Strategy.DockerStrategy.Env = append(buildConfig.Spec.Strategy.DockerStrategy.Env, w.Spec.ApplicationSource.Source2Image.Env...)
	}

	return buildConfig
}
