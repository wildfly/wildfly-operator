module github.com/wildfly/wildfly-operator

go 1.13

require (
	github.com/RHsyseng/operator-utils v1.4.5
	github.com/coreos/prometheus-operator v0.41.0
	github.com/go-logr/logr v0.1.0
	github.com/go-openapi/spec v0.19.9
	github.com/openshift/api v0.0.0-20200521101457-60c476765272
	github.com/operator-framework/operator-sdk v0.18.2
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	github.com/tevino/abool v1.2.0
	k8s.io/api v0.18.14
	k8s.io/apimachinery v0.18.14
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kube-openapi v0.0.0-20200410145947-61e04a5be9a6
	sigs.k8s.io/controller-runtime v0.6.2
)

replace (
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v13.3.2+incompatible // Required by OLM
	// OpenShift release-4.5
	github.com/openshift/api => github.com/openshift/api v0.0.0-20200526144822-34f54f12813a
	github.com/openshift/client-go => github.com/openshift/client-go v0.0.0-20200521150516-05eb9880269c
	// Pinned to kubernetes-1.18.2
	k8s.io/api => k8s.io/api v0.18.14
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.14
	k8s.io/client-go => k8s.io/client-go v0.18.14 // Required by prometheus-operator
)
