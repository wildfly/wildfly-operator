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
)

// GetOrCreateNewHeadlessService either returns the headless service or create it
func GetOrCreateNewHeadlessService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*corev1.Service, error) {
	headlessService := &corev1.Service{}
	if err := resources.Get(w, types.NamespacedName{Name: HeadlessServiceName(w), Namespace: w.Namespace}, client, headlessService); err != nil {
		if errors.IsNotFound(err) {
			servicePorts := []corev1.ServicePort{
				{
					Name: "http",
					Port: 8080,
				},
			}
			if err := resources.Create(w, client, scheme, newHeadlessService(w, labels, servicePorts)); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	return headlessService, nil
}

// GetOrCreateNewLoadBalancerService either returns the loadbalancer service or create it
func GetOrCreateNewLoadBalancerService(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*corev1.Service, error) {
	loadBalancerService := &corev1.Service{}
	if err := resources.Get(w, types.NamespacedName{Name: LoadBalancerServiceName(w), Namespace: w.Namespace}, client, loadBalancerService); err != nil {
		if errors.IsNotFound(err) {
			servicePorts := []corev1.ServicePort{
				{
					Name: "http",
					Port: 8080,
				},
			}
			if err := resources.Create(w, client, scheme, newLoadBalancerService(w, labels, servicePorts)); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	return loadBalancerService, nil
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
