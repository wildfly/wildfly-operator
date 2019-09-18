package resources

import (
	"reflect"
	"strconv"

	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// MarkerServerGeneration is an annotation to specifies the generation of the server that created underlying resources
	MarkerServerGeneration = "wildfly.org/wildfly-server-generation"
)

// IsCurrentGeneration returns true if the object has been created by the current generation of the WildFlyServer
func IsCurrentGeneration(w *wildflyv1alpha1.WildFlyServer, objectDefinition runtime.Object) bool {
	meta := objectDefinition.(metav1.Object)
	if generationStr, found := meta.GetAnnotations()[MarkerServerGeneration]; found {
		if generation, err := strconv.ParseInt(generationStr, 10, 64); err == nil {
			ok := generation == w.Generation
			if !ok {
				objectTypeString := reflect.TypeOf(objectDefinition).String()
				reqLogger := log.WithValues("WildFlyServer.Name", w.Name, "WildFlyServer.Namespace", w.Namespace)
				reqLogger.Info("Resource generations do not match", "Resource.Name", meta.GetName(), "Resource.Type", objectTypeString, "WildFlyServer.Generation", w.Generation, "Resource.Generation", generationStr)
			}
			return ok
		}
	}
	return false
}

// MarkServerGeneration adds a annotation to the object meta to specifies which generation of WildFlyServer created it.
func MarkServerGeneration(w *wildflyv1alpha1.WildFlyServer, meta *metav1.ObjectMeta) {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[MarkerServerGeneration] = strconv.FormatInt(w.Generation, 10)
}
