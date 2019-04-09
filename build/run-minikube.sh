#!/usr/bin/env bash

kubectl create -f deploy/rbac.yaml
# install WildFlyServer CRD
kubectl create -f deploy/crd.yaml
# install WildFly Operator
kubectl create -f deploy/operator.yaml