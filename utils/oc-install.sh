echo make sure that OpenShift is started 

oc login -u system:admin

oc adm policy add-cluster-role-to-user cluster-admin developer
oc login -u developer
oc create route edge --service=docker-registry -n default

oc project myproject
oc adm policy add-scc-to-user privileged -n myproject -z wildfly-operator
oc adm policy add-scc-to-user privileged -n myproject -z wildflyserver

oc apply -f deploy/service_account.yaml
oc apply -f deploy/role.yaml
oc apply -f deploy/role_binding.yaml
#oc apply -f deploy/security-context-constraints.yaml
oc apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_crd.yaml
oc apply -f deploy/operator.yaml

oc apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_cr.yaml
oc get all
oc describe wildlfyserver myapp-wildflyserver