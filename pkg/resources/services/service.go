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
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("wildlfyserver_services")

// CreateOrUpdateHeadlessService create a headless service or returns one up to date with the WildflyServer
func CreateOrUpdateHeadlessService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*corev1.Service, error) {
	headlessService := &corev1.Service{}
	err := resources.Get(w, types.NamespacedName{Name: HeadlessServiceName(w), Namespace: w.Namespace}, client, headlessService)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	// create the service if it is not found
	if errors.IsNotFound(err) {
		servicePorts := []corev1.ServicePort{
			{
				Name: "http",
				Port: resources.HTTPApplicationPort,
			},
		}
		if err := resources.Create(w, client, scheme, newHeadlessService(w, labels, servicePorts)); err != nil {
			return nil, err
		}
		return nil, nil
	}
	// service is found, update it if it does not match the wildlfyServer generation
	if !resources.IsCurrentGeneration(w, headlessService) {
		servicePorts := []corev1.ServicePort{
			{
				Name: "http",
				Port: resources.HTTPApplicationPort,
			},
		}
		if err := resources.Update(w, client, newHeadlessService(w, labels, servicePorts)); err != nil {
			// FIXME if update fails, delete
			return nil, err
		}
		return nil, nil
	}
	return headlessService, nil
}

// CreateOrUpdateLoadBalancerService create a loadbalancer service or returns one up to date with the WildflyServer
func CreateOrUpdateLoadBalancerService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*corev1.Service, error) {
	loadBalancer := &corev1.Service{}
	err := resources.Get(w, types.NamespacedName{Name: LoadBalancerServiceName(w), Namespace: w.Namespace}, client, loadBalancer)
	if err != nil && !errors.IsNotFound(err) {
		return nil, err
	}
	// create the service if it is not found
	if errors.IsNotFound(err) {
		servicePorts := []corev1.ServicePort{
			{
				Name: "http",
				Port: resources.HTTPApplicationPort,
			},
		}
		if err := resources.Create(w, client, scheme, newLoadBalancerService(w, labels, servicePorts)); err != nil {
			return nil, err
		}
		return nil, nil
	}
	// service is found, update it if it does not match the wildlfyServer generation
	if !resources.IsCurrentGeneration(w, loadBalancer) {
		servicePorts := []corev1.ServicePort{
			{
				Name: "http",
				Port: resources.HTTPApplicationPort,
			},
		}
		if err := resources.Update(w, client, newLoadBalancerService(w, labels, servicePorts)); err != nil {
			// FIXME if update fails, delete
			return nil, err
		}
		return nil, nil
	}
	return loadBalancer, nil
}

func newHeadlessService(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, servicePorts []corev1.ServicePort) *corev1.Service {
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
			Ports:     servicePorts,
		},
	}
	resources.MarkServerGeneration(w, &headlessService.ObjectMeta)
	return headlessService
}

// loadBalancerForWildFly returns a loadBalancer service
func newLoadBalancerService(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, servicePorts []corev1.ServicePort) *corev1.Service {
	sessionAffinity := corev1.ServiceAffinityNone
	if w.Spec.SessionAffinity {
		sessionAffinity = corev1.ServiceAffinityClientIP
	}
	loadBalancer := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LoadBalancerServiceName(w),
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:            corev1.ServiceTypeLoadBalancer,
			Selector:        labels,
			SessionAffinity: sessionAffinity,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: 8080,
				},
			},
		},
	}
	resources.MarkServerGeneration(w, &loadBalancer.ObjectMeta)
	return loadBalancer
}

// HeadlessServiceName returns the name of the headless service
func HeadlessServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-headless"
}

// LoadBalancerServiceName returns the name of the loadbalancer service
func LoadBalancerServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-loadbalancer"
}
