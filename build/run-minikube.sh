#!/usr/bin/env bash

kubectl apply -f deploy/service_account.yaml
kubectl apply -f deploy/role.yaml
kubectl apply -f deploy/role_binding.yaml
kubectl apply -f deploy/cluster_role.yaml
kubectl apply -f deploy/cluster_role_binding.yaml
# install WildFlyServer CRD
kubectl apply -f deploy/crds/wildfly.org_wildflyservers_crd_with_webhook.yaml
# install WildFly Operator
kubectl create -f deploy/operator.yaml