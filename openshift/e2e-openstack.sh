#!/bin/bash

set -euo pipefail

echo "Running e2e-openstack.sh"

unset GOFLAGS
tmp="$(mktemp -d)"

# Default branch for both CAPO and this repo should be `main`.
CAPO_BASE_REF=$PULL_BASE_REF

echo "cloning github.com/openshift/cluster-api-provider-openstack at branch '$CAPO_BASE_REF'"
git clone --single-branch --branch="$CAPO_BASE_REF" --depth=1 "https://github.com/openshift/cluster-api-provider-openstack.git" "$tmp"

echo "running cluster-api-provider-openstack's: make -C 'openshift' e2e"
exec make -C "$tmp/openshift" e2e
