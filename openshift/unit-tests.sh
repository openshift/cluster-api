#!/bin/bash

set -o errexit
set -o pipefail

echo "Running unit-tests.sh"

# Ensure that some home var is set and that it's not the root
export HOME=${HOME:=/tmp/kubebuilder/testing}
if [ $HOME == "/" ]; then
  export HOME=/tmp/kubebuilder/testing
fi

KUBEBUILDER_ASSETS=$(make --no-print-directory setup-envtest | sed -n 's/^KUBEBUILDER_ASSETS=//p')
if [ -z "$KUBEBUILDER_ASSETS" ]; then
  echo "Failed to resolve KUBEBUILDER_ASSETS from make setup-envtest" >&2
  exit 1
fi
export KUBEBUILDER_ASSETS
go test ./api/...
go test ./bootstrap/...
go test ./cmd/...
make -C openshift test-capi-ipam-conversion-webhook
go test ./controllers/...
go test ./controlplane/...
go test ./exp/...
