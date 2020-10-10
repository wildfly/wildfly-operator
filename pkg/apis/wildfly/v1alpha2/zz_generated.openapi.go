// +build !ignore_autogenerated

// Code generated by openapi-gen. DO NOT EDIT.

// This file was autogenerated by openapi-gen. Do not edit it manually!

package v1alpha2

import (
	spec "github.com/go-openapi/spec"
	common "k8s.io/kube-openapi/pkg/common"
)

func GetOpenAPIDefinitions(ref common.ReferenceCallback) map[string]common.OpenAPIDefinition {
	return map[string]common.OpenAPIDefinition{
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.ConfigMapSpec":           schema_pkg_apis_wildfly_v1alpha2_ConfigMapSpec(ref),
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.PodStatus":               schema_pkg_apis_wildfly_v1alpha2_PodStatus(ref),
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.StandaloneConfigMapSpec": schema_pkg_apis_wildfly_v1alpha2_StandaloneConfigMapSpec(ref),
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.StorageSpec":             schema_pkg_apis_wildfly_v1alpha2_StorageSpec(ref),
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServer":           schema_pkg_apis_wildfly_v1alpha2_WildFlyServer(ref),
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServerSpec":       schema_pkg_apis_wildfly_v1alpha2_WildFlyServerSpec(ref),
		"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServerStatus":     schema_pkg_apis_wildfly_v1alpha2_WildFlyServerStatus(ref),
	}
}

func schema_pkg_apis_wildfly_v1alpha2_ConfigMapSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "ConfigMapSpec represents a ConfigMap definition with a name and a mount path. It can optionally specify the path, as an absolute or relative path, within the container at which the volume should be mounted. If the specified mount path is a relative path, then it will be treated relative to JBOSS_HOME. If a MountPath is not specified, then the ConfigMap is mount by default into /etc/configmaps/<configmap-name>. the MountPath cannot contains ':' character.",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"name": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"mountPath": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
				},
				Required: []string{"name"},
			},
		},
	}
}

func schema_pkg_apis_wildfly_v1alpha2_PodStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "PodStatus defines the observed state of pods running the WildFlyServer application",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"name": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"podIP": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"state": {
						SchemaProps: spec.SchemaProps{
							Description: "Represent the state of the Pod, it is used especially during scale down.",
							Type:        []string{"string"},
							Format:      "",
						},
					},
				},
				Required: []string{"name", "podIP", "state"},
			},
		},
	}
}

func schema_pkg_apis_wildfly_v1alpha2_StandaloneConfigMapSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "StandaloneConfigMapSpec defines the desired configMap configuration to obtain the standalone configuration for WildFlyServer",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"name": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"key": {
						SchemaProps: spec.SchemaProps{
							Description: "Key of the config map whose value is the standalone XML configuration file (\"standalone.xml\" if omitted)",
							Type:        []string{"string"},
							Format:      "",
						},
					},
				},
				Required: []string{"name"},
			},
		},
	}
}

func schema_pkg_apis_wildfly_v1alpha2_StorageSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "StorageSpec defines the desired storage for WildFlyServer",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"emptyDir": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/api/core/v1.EmptyDirVolumeSource"),
						},
					},
					"volumeClaimTemplate": {
						SchemaProps: spec.SchemaProps{
							Description: "VolumeClaimTemplate defines the template to store WildFlyServer standalone data directory. The name of the template is derived from the WildFlyServer name.\n The corresponding volume will be mounted in ReadWriteOnce access mode.\nThis template should be used to specify specific Resources requirements in the template spec.",
							Ref:         ref("k8s.io/api/core/v1.PersistentVolumeClaim"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"k8s.io/api/core/v1.EmptyDirVolumeSource", "k8s.io/api/core/v1.PersistentVolumeClaim"},
	}
}

func schema_pkg_apis_wildfly_v1alpha2_WildFlyServer(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "WildFlyServer is the Schema for the wildflyservers API",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"kind": {
						SchemaProps: spec.SchemaProps{
							Description: "Kind is a string value representing the REST resource this object represents. Servers may infer this from the endpoint the client submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"apiVersion": {
						SchemaProps: spec.SchemaProps{
							Description: "APIVersion defines the versioned schema of this representation of an object. Servers should convert recognized schemas to the latest internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"metadata": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"),
						},
					},
					"spec": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServerSpec"),
						},
					},
					"status": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServerStatus"),
						},
					},
				},
			},
		},
		Dependencies: []string{
			"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServerSpec", "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.WildFlyServerStatus", "k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta"},
	}
}

func schema_pkg_apis_wildfly_v1alpha2_WildFlyServerSpec(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "WildFlyServerSpec defines the desired state of WildFlyServer",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"applicationImage": {
						SchemaProps: spec.SchemaProps{
							Description: "ApplicationImage is the name of the application image to be deployed",
							Type:        []string{"string"},
							Format:      "",
						},
					},
					"replicas": {
						SchemaProps: spec.SchemaProps{
							Description: "Replicas is the desired number of replicas for the application",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
					"sessionAffinity": {
						SchemaProps: spec.SchemaProps{
							Description: "SessionAffinity defines if connections from the same client ip are passed to the same WildFlyServer instance/pod each time (false if omitted)",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"disableHTTPRoute": {
						SchemaProps: spec.SchemaProps{
							Description: "DisableHTTPRoute disables the creation a route to the HTTP port of the application service (false if omitted)",
							Type:        []string{"boolean"},
							Format:      "",
						},
					},
					"standaloneConfigMap": {
						SchemaProps: spec.SchemaProps{
							Ref: ref("github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.StandaloneConfigMapSpec"),
						},
					},
					"storage": {
						SchemaProps: spec.SchemaProps{
							Description: "StorageSpec defines specific storage required for the server own data directory. If omitted, an EmptyDir is used (that will not persist data across pod restart).",
							Ref:         ref("github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.StorageSpec"),
						},
					},
					"serviceAccountName": {
						SchemaProps: spec.SchemaProps{
							Type:   []string{"string"},
							Format: "",
						},
					},
					"envFrom": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "atomic",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "EnvFrom contains environment variables from a source such as a ConfigMap or a Secret",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("k8s.io/api/core/v1.EnvFromSource"),
									},
								},
							},
						},
					},
					"env": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "atomic",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "Env contains environment variables for the containers running the WildFlyServer application",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("k8s.io/api/core/v1.EnvVar"),
									},
								},
							},
						},
					},
					"secrets": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "set",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "Secrets is a list of Secrets in the same namespace as the WildFlyServer object, which shall be mounted into the WildFlyServer Pods. The Secrets are mounted into /etc/secrets/<secret-name>.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Type:   []string{"string"},
										Format: "",
									},
								},
							},
						},
					},
					"configMaps": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "atomic",
							},
						},
						SchemaProps: spec.SchemaProps{
							Description: "ConfigMaps is a list of ConfigMaps in the same namespace as the WildFlyServer object, which shall be mounted into the WildFlyServer Pods.",
							Type:        []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.ConfigMapSpec"),
									},
								},
							},
						},
					},
				},
				Required: []string{"applicationImage", "replicas"},
			},
		},
		Dependencies: []string{
			"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.ConfigMapSpec", "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.StandaloneConfigMapSpec", "github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.StorageSpec", "k8s.io/api/core/v1.EnvFromSource", "k8s.io/api/core/v1.EnvVar"},
	}
}

func schema_pkg_apis_wildfly_v1alpha2_WildFlyServerStatus(ref common.ReferenceCallback) common.OpenAPIDefinition {
	return common.OpenAPIDefinition{
		Schema: spec.Schema{
			SchemaProps: spec.SchemaProps{
				Description: "WildFlyServerStatus defines the observed state of WildFlyServer",
				Type:        []string{"object"},
				Properties: map[string]spec.Schema{
					"replicas": {
						SchemaProps: spec.SchemaProps{
							Description: "Replicas is the actual number of replicas for the application",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
					"pods": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "atomic",
							},
						},
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Ref: ref("github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.PodStatus"),
									},
								},
							},
						},
					},
					"hosts": {
						VendorExtensible: spec.VendorExtensible{
							Extensions: spec.Extensions{
								"x-kubernetes-list-type": "set",
							},
						},
						SchemaProps: spec.SchemaProps{
							Type: []string{"array"},
							Items: &spec.SchemaOrArray{
								Schema: &spec.Schema{
									SchemaProps: spec.SchemaProps{
										Type:   []string{"string"},
										Format: "",
									},
								},
							},
						},
					},
					"scalingdownPods": {
						SchemaProps: spec.SchemaProps{
							Description: "Represents the number of pods which are in scaledown process what particular pod is scaling down can be verified by PodStatus\n\nRead-only.",
							Type:        []string{"integer"},
							Format:      "int32",
						},
					},
				},
				Required: []string{"replicas", "scalingdownPods"},
			},
		},
		Dependencies: []string{
			"github.com/wildfly/wildfly-operator/pkg/apis/wildfly/v1alpha2.PodStatus"},
	}
}