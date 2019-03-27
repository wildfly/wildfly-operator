kubectl create -f deploy/service_account.yaml
kubectl create -f deploy/role.yaml
kubectl create -f deploy/role_binding.yaml

# install WildFlyServer CRD
kubectl create -f deploy/crds/wildfly_v1alpha1_wildflyserver_crd.yaml
# install WildFly Operator
kubectl create -f deploy/operator.yaml

# install Custom WildFlyServer resource
kubectl create -f deploy/crds/wildfly_v1alpha1_wildflyserver_cr.yaml

echo
echo
echo "application is accessible from $(minikube service myapp-wildflyserver-loadbalancer --url)"

