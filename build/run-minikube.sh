#!/usr/bin/env bash

kubectl apply -f deploy/service_account.yaml
kubectl apply -f deploy/role.yaml
kubectl apply -f deploy/role_binding.yaml
# install WildFlyServer CRD
kubectl apply -f deploy/crds/wildfly.org_wildflyservers_crd.yaml
# install WildFly Operator
kubectl create -f deploy/operator.yaml