package v1alpha2

import (
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var log = logf.Log.WithName("webhook")

var (
	hookServerPort = 8443
)

func (r *WildFlyServer) SetupWebhookWithManager(mgr ctrl.Manager) error {

	hookServer := mgr.GetWebhookServer()
	hookServer.Port = hookServerPort

	log.Info("Initializing the WebHook", "Port: ", hookServerPort)

	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// var _ webhook.Defaulter = &WildFlyServer{}

// Default implements webhook.Defaulter so a webhook be registered for the WildFlyServer type
func (r *WildFlyServer) Default() {
	log.Info("default converting values", "name", r.Name)
}
