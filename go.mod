module github.com/wildfly/wildfly-operator

go 1.16

require (
	github.com/Azure/go-autorest/autorest v0.11.10 // indirect
	github.com/RHsyseng/operator-utils v1.4.7
	github.com/go-logr/logr v0.4.0
	github.com/go-openapi/swag v0.19.10 // indirect
	github.com/onsi/ginkgo/v2 v2.1.3
	github.com/onsi/gomega v1.18.1
	github.com/openshift/api v0.0.0-20210928121311-b64fe3d0dc32
	github.com/operator-framework/operator-lib v0.3.0
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.50.0
	github.com/prometheus/client_golang v1.8.0 // indirect
	github.com/prometheus/common v0.14.0
	github.com/stretchr/testify v1.7.0
	github.com/tevino/abool v1.2.0
	golang.org/x/oauth2 v0.0.0-20200902213428-5d25da1a8d43 // indirect
	k8s.io/api v0.20.4
	k8s.io/apimachinery v0.20.4
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/kube-openapi v0.0.0-20220124234850-424119656bbf
	k8s.io/utils v0.0.0-20210802155522-efc7438f0176
	sigs.k8s.io/controller-runtime v0.8.3
)

replace (
	k8s.io/api => k8s.io/api v0.19.14
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.14
	k8s.io/client-go => k8s.io/client-go v0.19.14
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.8.3
)
