package servicemonitors

import (
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("wildlfyserver_servicemonitors")

// GetOrCreateNewServiceMonitor either returns the headless service or create it
func GetOrCreateNewServiceMonitor(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*monitoringv1.ServiceMonitor, error) {
	serviceMonitor := &monitoringv1.ServiceMonitor{}
	if err := resources.Get(w, types.NamespacedName{Name: w.Name, Namespace: w.Namespace}, client, serviceMonitor); err != nil {
		if errors.IsNotFound(err) {
			if err := resources.Create(w, client, scheme, newServiceMonitor(w, labels)); err != nil {
				if errors.IsAlreadyExists(err) {
					return nil, nil
				}
				return nil, err
			}
			return nil, nil
		}
	}
	return serviceMonitor, nil
}

func newServiceMonitor(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *monitoringv1.ServiceMonitor {
	return &monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      w.Name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Endpoints: []monitoringv1.Endpoint{{
				Port: "admin",
			}},
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
		},
	}
}
