#!/usr/bin/env bash

kubectl apply -f deploy/service_account.yaml
kubectl apply -f deploy/role.yaml
kubectl apply -f deploy/role_binding.yaml
# install WildFlyServer CRD
kubectl apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_crd.yaml
# install WildFly Operator
kubectl create -f deploy/operator.yaml