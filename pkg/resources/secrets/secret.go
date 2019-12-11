package secrets

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	"github.com/wildfly/wildfly-operator/pkg/resources"

	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("wildlfyserver_secrets")

// GetOrCreateNewSecret either returns the secret or create it
func GetOrCreateNewSecret(w *wildflyv1alpha1.WildFlyServer, client client.Client, scheme *runtime.Scheme, labels map[string]string, name string) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := resources.Get(w, types.NamespacedName{Name: name, Namespace: w.Namespace}, client, secret); err != nil {
		if errors.IsNotFound(err) {
			if err := resources.Create(w, client, scheme, NewSecret(w, labels, name)); err != nil {
				return nil, err
			}
			return nil, nil
		}
	}
	return secret, nil
}

// NewSecret returs a new Secret
func NewSecret(w *wildflyv1alpha1.WildFlyServer, labels map[string]string, name string) *corev1.Secret {
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "core/v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: w.Namespace,
			Labels:    labels,
		},
	}
	return secret
}