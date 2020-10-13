#!/usr/bin/env bash

oc adm policy add-cluster-role-to-user cluster-admin developer
oc apply -f deploy/service_account.yaml
oc apply -f deploy/role.yaml
oc apply -f deploy/role_binding.yaml
oc apply -f deploy/cluster_role.yaml
oc apply -f deploy/cluster_role_binding.yaml
oc apply -f deploy/crds/wildfly.org_wildflyservers_crd_with_webhook.yaml
oc apply -f deploy/operator.yaml
