package services

import (
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewHeadlessService creates a new corev1.Service that exposes the servicePorts.
func NewHeadlessService(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, servicePorts []corev1.ServicePort) *corev1.Service {
	headlessService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      headlessServiceName(w),
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

func headlessServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-headless"
}
