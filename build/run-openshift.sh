#!/usr/bin/env bash

oc login -u system:admin
oc apply -f deploy/rbac.yaml
oc apply -f deploy/crd.yaml
oc apply -f deploy/operator.yaml
oc login -u developer