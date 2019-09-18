package resources

import (
	"context"
	"reflect"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("wildlfyserver_resources")

// Create creates a new resource from the objectDefinition and set w as its ControllerReference
func Create(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, objectDefinition runtime.Object) error {

	reqLogger := log.WithValues("WildFlyServer Name", w.Name, "WildFlyServer Namespace", w.Namespace)
	objectTypeString := reflect.TypeOf(objectDefinition).String()
	meta := objectDefinition.(metav1.Object)
	reqLogger.Info("Creating new " + meta.GetName() + "  " + objectTypeString)

	var err error
	if err = controllerutil.SetControllerReference(w, meta, scheme); err != nil {
		reqLogger.Error(err, "Failed to set controller reference for new "+objectTypeString)
	}
	reqLogger.Info("Set controller reference for new " + objectTypeString)

	if err = client.Create(context.TODO(), objectDefinition); err != nil {
		reqLogger.Error(err, "Failed to create new "+meta.GetName()+"  "+objectTypeString)
	}
	reqLogger.Info("Created " + meta.GetName() + " " + objectTypeString)

	return err
}

// Get returns the object from the objectDefinition
func Get(w *wildflyv1alpha1.WildFlyServer, namespacedName types.NamespacedName, client client.Client, objectDefinition runtime.Object) error {
	reqLogger := log.WithValues("WildFlyServer Name", w.Name, "WildFlyServer Namespace", w.Namespace)
	objectTypeString := reflect.TypeOf(objectDefinition).String()
	reqLogger.Info("Getting " + namespacedName.Name + "  " + objectTypeString)

	var err error
	if err = client.Get(context.TODO(), namespacedName, objectDefinition); err != nil {
		if errors.IsNotFound(err) || runtime.IsNotRegisteredError(err) {
			reqLogger.Info(namespacedName.Name + " " + objectTypeString + " is not found")
		}
	} else {
		reqLogger.Info("Found " + namespacedName.Name + " " + objectTypeString)
	}
	return err
}

// Update updates the resource specified by the objectDefinition.
func Update(w *wildflyv1alpha1.WildFlyServer, client client.Client, objectDefinition runtime.Object) error {

	reqLogger := log.WithValues("WildFlyServer Name", w.Name, "WildFlyServer Namespace", w.Namespace)
	objectTypeString := reflect.TypeOf(objectDefinition).String()
	meta := objectDefinition.(metav1.Object)
	reqLogger.Info("Updating " + meta.GetName() + "  " + objectTypeString)

	var err error
	if err = client.Update(context.TODO(), objectDefinition); err != nil {
		reqLogger.Error(err, "Failed to update "+meta.GetName()+"  "+objectTypeString)
	}

	return err
}

// UpdateStatus updates status of the resource specified by the objectDefinition.
func UpdateStatus(w *wildflyv1alpha1.WildFlyServer, client client.Client, objectDefinition runtime.Object) error {

	reqLogger := log.WithValues("WildFlyServer Name", w.Name, "WildFlyServer Namespace", w.Namespace)
	objectTypeString := reflect.TypeOf(objectDefinition).String()
	reqLogger.Info("Updating status of " + objectTypeString)

	var err error
	if err = client.Status().Update(context.Background(), objectDefinition); err != nil {
		reqLogger.Error(err, "Failed to update status of "+objectTypeString)
	}

	return err
}

// Delete deletes the resource specified by the objectDefinition.
func Delete(w *wildflyv1alpha1.WildFlyServer, client client.Client, objectDefinition runtime.Object) error {

	reqLogger := log.WithValues("WildFlyServer Name", w.Name, "WildFlyServer Namespace", w.Namespace)
	objectTypeString := reflect.TypeOf(objectDefinition).String()
	reqLogger.Info("Deleting " + objectTypeString)

	var err error
	if err = client.Delete(context.TODO(), objectDefinition); err != nil {
		reqLogger.Error(err, "Failed to delete "+objectTypeString)
	}

	return err
}
