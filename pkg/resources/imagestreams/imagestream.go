package imagestreams

import (
	imagev1 "github.com/openshift/api/image/v1"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetOrCreateNewBuilderImageStream either returns the ImageStream for the builder image or create it
func GetOrCreateNewBuilderImageStream(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*imagev1.ImageStream, error) {
	return getOrCreateNewImageStream(w, client, scheme, labels, newImageStream, w.Name)
}

// GetOrCreateNewRuntimeImageStream either returns the ImageStream for the runtime image or create it
func GetOrCreateNewRuntimeImageStream(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*imagev1.ImageStream, error) {
	return getOrCreateNewImageStream(w, client, scheme, labels, newImageStream, w.Name+"-runtime")
}

func getOrCreateNewImageStream(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string, createImageStream func(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, name string) *imagev1.ImageStream, name string) (*imagev1.ImageStream, error) {
	imageStream := &imagev1.ImageStream{}
	err := resources.Get(w, types.NamespacedName{Name: name, Namespace: w.Namespace}, client, imageStream)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	// create the imageStream if it is not found
	if errors.IsNotFound(err) {
		if err := resources.Create(w, client, scheme, createImageStream(w, labels, name)); err != nil {
			if errors.IsAlreadyExists(err) {
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	}

	// imageStream is found, update it if it does not match the wildlfyServer generation
	if !resources.IsCurrentGeneration(w, imageStream) {
		newImageStream := createImageStream(w, labels, name)
		imageStream.Labels = labels
		imageStream.Spec = newImageStream.Spec

		if err := resources.Update(w, client, imageStream); err != nil {
			if errors.IsInvalid(err) {
				// Can not update, so we delete to recreate the imageStream from scratch
				if err := resources.Delete(w, client, imageStream); err != nil {
					return nil, err
				}
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	}

	return imageStream, nil
}

func newImageStream(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, name string) *imagev1.ImageStream {
	imageStream := &imagev1.ImageStream{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "image.openshift.io/v1",
			Kind:       "ImageStream",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
	}

	return imageStream
}
