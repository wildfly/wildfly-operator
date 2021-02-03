#!/usr/bin/env bash

# install WildFlyServer CRD
kubectl apply -f deploy/crds/wildfly.org_wildflyservers_crd.yaml
# install WildFly Operator (plus service accounts and role binding)
kubectl create -f deploy/operator.yaml
