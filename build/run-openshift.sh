#!/usr/bin/env bash

oc adm policy add-cluster-role-to-user cluster-admin developer
oc apply -f deploy/crds/wildfly.org_wildflyservers_crd.yaml
oc apply -f deploy/operator.yaml
