package services

import (
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("wildflyserver_services")

// CreateOrUpdateAdminService create a admin service or returns one up to date with the WildflyServer
func CreateOrUpdateAdminService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*corev1.Service, error) {
	return createOrUpdateService(w, client, scheme, labels, AdminServiceName(w), newAdminService)
}

// CreateOrUpdateHeadlessService create a headless service or returns one up to date with the WildflyServer
func CreateOrUpdateHeadlessService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme,
	labels map[string]string) (*corev1.Service, error) {
	return createOrUpdateService(w, client, scheme, labels, HeadlessServiceName(w), newHeadlessService)
}

// CreateOrUpdateClusterService create a clusterIP service or returns one up to date with the WildflyServer
func CreateOrUpdateClusterService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*corev1.Service, error) {
	return createOrUpdateService(w, client, scheme, labels, ClusterServiceName(w), newClusterService)
}

// createOrUpdateAdminService create a service or returns one up to date with the WildflyServer.
// The serviceCreator function is responsible for the actual creation of the corev1.Service object
func createOrUpdateService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme,
	labels map[string]string,
	serviceName string,
	serviceCreator func(*wildflyv1alpha1.WildFlyServer, map[string]string) *corev1.Service) (*corev1.Service, error) {
	labels[resources.MarkerOperatedByHeadless] = resources.MarkerServiceActive // managing only active pods which are permitted to run EJB remote calls
	service := &corev1.Service{}
	err := resources.Get(w, types.NamespacedName{Name: serviceName, Namespace: w.Namespace}, client, service)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	// create the service if it is not found
	if errors.IsNotFound(err) {
		if err := resources.Create(w, client, scheme, serviceCreator(w, labels)); err != nil {
			if errors.IsAlreadyExists(err) {
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	}
	// service is found, update it if it does not match the wildflyServer generation
	if !resources.IsCurrentGeneration(w, service) {
		newService := serviceCreator(w, labels)
		// copy the ClusterIP that was set after the route is created.
		newService.Spec.ClusterIP = service.Spec.ClusterIP
		service.Labels = labels
		service.Spec = newService.Spec

		if err := resources.Update(w, client, service); err != nil {
			if errors.IsInvalid(err) {
				// Can not update, so we delete to recreate the service from scratch
				if err := resources.Delete(w, client, service); err != nil {
					return nil, err
				}
				return nil, nil
			}
			return nil, err
		}
		return nil, nil
	}
	return service, nil
}

func newHeadlessService(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *corev1.Service {
	labels[resources.MarkerOperatedByHeadless] = resources.MarkerServiceActive // managing only active pods which are permitted to run EJB remote calls
	headlessService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HeadlessServiceName(w),
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			Selector:  labels,
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: resources.HTTPApplicationPort,
				},
			},
		},
	}
	return headlessService
}

// newAdminService returns a service exposing the management port of WildFly
func newAdminService(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *corev1.Service {
	headlessService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AdminServiceName(w),
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			Selector:  labels,
			ClusterIP: corev1.ClusterIPNone,
			Ports: []corev1.ServicePort{
				{
					Name: "admin",
					Port: resources.HTTPManagementPort,
				},
			},
		},
	}
	return headlessService
}

// newClusterService returns a ClusterIP service
func newClusterService(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *corev1.Service {
	labels[resources.MarkerOperatedByLoadbalancer] = resources.MarkerServiceActive // managing only active pods which are not in scaledown process
	sessionAffinity := corev1.ServiceAffinityNone
	if w.Spec.SessionAffinity {
		sessionAffinity = corev1.ServiceAffinityClientIP
	}
	loadBalancer := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ClusterServiceName(w),
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeClusterIP,
			Selector:        labels,
			SessionAffinity: sessionAffinity,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: resources.HTTPApplicationPort,
				},
			},
		},
	}
	return loadBalancer
}

// HeadlessServiceName returns the name of the headless service
func HeadlessServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-headless"
}

// AdminServiceName returns the name of the admin service
func AdminServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-admin"
}

// ClusterServiceName returns the name of the cluster service.
//
// The service remains named with the -loadbalancer suffix for backwards compatibility
/// even if it is now a ClusterIP service.
func ClusterServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-loadbalancer"
}
