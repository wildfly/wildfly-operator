// +build !unit

package e2e

import (
	"testing"
	"time"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/wildfly/wildfly-operator/pkg/apis"
	wildflyv1alpha1 "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha1"
	wildflyframework "github.com/wildfly/wildfly-operator/test/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	retryInterval        = time.Second * 5
	timeout              = time.Minute * 3
	cleanupRetryInterval = time.Second * 1
	cleanupTimeout       = time.Second * 5
)

func TestWildFly17Server(t *testing.T) {
	wildflyServerList := &wildflyv1alpha1.WildFlyServerList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WildFlyServer",
			APIVersion: "wildfly.org/v1alpha1",
		},
	}
	err := framework.AddToFrameworkScheme(apis.AddToScheme, wildflyServerList)
	if err != nil {
		t.Fatalf("failed to add custom resource scheme to framework: %v", err)
	}
	// run subtests
	t.Run("BasicTest", wildFlyBasicTest)
	t.Run("ClusterTest", wildFlyClusterTest)
}

func wildFlyBasicTest(t *testing.T) {
	wildflyframework.WildFlyBasicTest(t, "17.0")
}

func wildFlyClusterTest(t *testing.T) {
	wildflyframework.WildFlyClusterTest(t, "17.0")
}
