#!/bin/bash

set -euo pipefail

echo "Running e2e-openstack.sh"

unset GOFLAGS
tmp="$(mktemp -d)"

if [ "${PULL_BASE_REF}" == "master" ]; then
  # the default branch for cluster-api-provider-openstack is main.
  CAPO_BASE_REF="main"
else
  CAPO_BASE_REF=$PULL_BASE_REF
fi

echo "cloning github.com/openshift/cluster-api-provider-openstack at branch '$CAPO_BASE_REF'"
git clone --single-branch --branch="$CAPO_BASE_REF" --depth=1 "https://github.com/openshift/cluster-api-provider-openstack.git" "$tmp"

echo "running cluster-api-provider-openstack's: make -C 'openshift' e2e"
exec make -C "$tmp/openshift" e2e
