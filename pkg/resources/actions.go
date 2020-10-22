package resources

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("wildlfyserver_resources")

// Create creates a new resource from the objectDefinition and set w as its ControllerReference
func Create(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, objectDefinition runtime.Object) error {
	logger := logWithValues(w, objectDefinition)
	logger.Info("Creating resource")

	meta := objectDefinition.(metav1.Object)
	// mark the object with the current server generation
	MarkServerGeneration(w, meta)

	if err := controllerutil.SetControllerReference(w, meta, scheme); err != nil {
		logger.Error(err, "Failed to set controller reference for new resource")
		return err
	}
	logger.Info("Set controller reference for new resource")

	if err := client.Create(context.TODO(), objectDefinition); err != nil {
		return err
	}

	logger.Info("Created resource")
	return nil
}

// Get returns the object from the objectDefinition
func Get(w *wildflyv1alpha1.WildFlyServer, namespacedName types.NamespacedName, client client.Client, objectDefinition runtime.Object) error {
	logger := log.WithValues("WildFlyServer.Namespace", w.Namespace, "WildFlyServer.Name", w.Name, "Resource.Name", namespacedName.Name)
	logger.Info("Getting resource")

	if err := client.Get(context.TODO(), namespacedName, objectDefinition); err != nil {
		if errors.IsNotFound(err) || runtime.IsNotRegisteredError(err) {
			logger.Info("Resource not found")
		}
		return err
	}

	logger.Info("Got resource")
	return nil
}

// Update updates the resource specified by the objectDefinition.
func Update(w *wildflyv1alpha1.WildFlyServer, client client.Client, objectDefinition runtime.Object) error {
	logger := logWithValues(w, objectDefinition)
	logger.Info("Updating Resource")

	meta := objectDefinition.(metav1.Object)
	// mark the object with the current wildlyserver generation unless it is the wildlyserver itself
	if objectDefinition != w {
		MarkServerGeneration(w, meta)
	}

	if err := client.Update(context.TODO(), objectDefinition); err != nil {
		logger.Error(err, "Failed to update resource")
		return err
	}

	logger.Info("Updated resource")
	return nil
}

// UpdateStatus updates status of the resource specified by the objectDefinition.
func UpdateStatus(w *wildflyv1alpha1.WildFlyServer, client client.Client, objectDefinition runtime.Object) error {
	logger := logWithValues(w, objectDefinition)
	logger.Info("Updating status of resource")

	if err := client.Status().Update(context.Background(), objectDefinition); err != nil {
		logger.Error(err, "Failed to update status of resource")
		return err
	}

	logger.Info("Updated status of resource")
	return nil
}

// UpdateWildFlyServerStatus updates status of the WildFlyServer resource.
func UpdateWildFlyServerStatus(w *wildflyv1alpha1.WildFlyServer, client client.Client) error {
	logger := log.WithValues("WildFlyServer.Namespace", w.Namespace, "WildFlyServer.Name", w.Name)
	logger.Info("Updating status of WildFlyServer")

	if err := client.Status().Update(context.Background(), w); err != nil {
		logger.Error(err, "Failed to update status of WildFlyServer")
		return err
	}

	logger.Info("Updated status of WildFlyServer")
	return nil
}

// Delete deletes the resource specified by the objectDefinition.
func Delete(w *wildflyv1alpha1.WildFlyServer, client client.Client, objectDefinition runtime.Object) error {
	logger := logWithValues(w, objectDefinition)
	logger.Info("Deleting Resource")

	if err := client.Delete(context.TODO(), objectDefinition); err != nil {
		logger.Error(err, "Failed to delete  resource")
		return err
	}

	logger.Info("Deleted resource")
	return nil
}

func logWithValues(w *wildflyv1alpha1.WildFlyServer, objectDefinition runtime.Object) logr.Logger {
	objectTypeString := reflect.TypeOf(objectDefinition).String()
	meta := objectDefinition.(metav1.Object)
	return log.WithValues("WildFlyServer.Namespace", w.Namespace, "WildFlyServer.Name", w.Name, "Resource.Name", meta.GetName(), "Resource.Type", objectTypeString)
}

// CustomResourceDefinitionExists returns true if the CRD exists in the cluster
func CustomResourceDefinitionExists(gvk schema.GroupVersionKind) bool {
	cfg, err := config.GetConfig()
	if err != nil {
		return false
	}
	client, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return false
	}
	api, err := client.ServerResourcesForGroupVersion(gvk.GroupVersion().String())
	if err != nil {
		return false
	}
	for _, a := range api.APIResources {
		if a.Kind == gvk.Kind {
			return true
		}
	}
	return false
}
