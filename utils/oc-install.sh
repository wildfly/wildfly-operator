# OpenShift requires additional steps
#
# The examples will be installed in a project named `myproject`.

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
oc apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_crd.yaml
oc apply -f deploy/operator.yaml

oc apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_cr.yaml
oc expose svc/myapp-wildflyserver-loadbalancer

echo
echo
echo "application is accessible from http://$(oc get route myapp-wildflyserver-loadbalancer --template='{{ .spec.host }}')"