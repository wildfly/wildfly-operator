#!/usr/bin/env bash

oc login -u system:admin
oc adm policy add-cluster-role-to-user cluster-admin developer
oc apply -f deploy/service_account.yaml
oc apply -f deploy/role.yaml
oc apply -f deploy/role_binding.yaml
oc apply -f deploy/crds/wildfly_v1alpha1_wildflyserver_crd.yaml
oc apply -f deploy/operator.yaml
oc login -u developer