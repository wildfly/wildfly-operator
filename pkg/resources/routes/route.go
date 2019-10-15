package routes

import (
	routev1 "github.com/openshift/api/route/v1"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"
	"github.com/wildfly/wildfly-operator/pkg/resources/services"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetOrCreateNewRoute either returns the headless service or create it
func GetOrCreateNewRoute(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string) (*routev1.Route, error) {
	route := &routev1.Route{}
	if err := resources.Get(w, types.NamespacedName{Name: routeServiceName(w), Namespace: w.Namespace}, client, route); err != nil {
		if errors.IsNotFound(err) {
			if err := resources.Create(w, client, scheme, newRoute(w, labels)); err != nil {
				if errors.IsAlreadyExists(err) {
					return nil, nil
				}
				return nil, err
			}
			return nil, nil
		}
	}
	return route, nil
}

// DeleteExistingRoute delete the route if it exists. It returns true if the route is deleted.
func DeleteExistingRoute(w *wildflyv1alpha1.WildFlyServer, client client.Client) (bool, error) {
	route := &routev1.Route{}
	if err := resources.Get(w, types.NamespacedName{Name: routeServiceName(w), Namespace: w.Namespace}, client, route); err != nil {
		if errors.IsNotFound(err) {
			// route has been not found, nothing to do
			return false, nil
		}
		return false, err
	}
	// route has been found, let's delete it
	if err := resources.Delete(w, client, route); err != nil {
		return true, err
	}
	return false, nil
}

func newRoute(w *wildflyv1alpha1.WildFlyServer, labels map[string]string) *routev1.Route {
	weight := int32(100)

	route := &routev1.Route{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "route.openshift.io/v1",
			Kind:       "Route",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeServiceName(w),
			Namespace: w.Namespace,
			Labels:    labels,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   services.LoadBalancerServiceName(w),
				Weight: &weight,
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.FromString("http"),
			},
		},
	}

	return route
}

// routeServiceName returns the name of the HTTP route
func routeServiceName(w *wildflyv1alpha1.WildFlyServer) string {
	return w.Name + "-route"
}
