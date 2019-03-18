kubectl delete -f deploy/crds/
kubectl delete -f deploy/

# check that the are no remaining resources 
kubectl get all

