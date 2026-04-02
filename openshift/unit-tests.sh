#!/bin/bash

set -o errexit
set -o pipefail

echo "Running unit-tests.sh"

# Ensure that some home var is set and that it's not the root
export HOME=${HOME:=/tmp/kubebuilder/testing}
if [ $HOME == "/" ]; then
  export HOME=/tmp/kubebuilder/testing
fi

# Match Makefile `test` / `envtest`: download envtest assets (no setup-envtest binary).
export ENVTEST_ASSETS_DIR="${ENVTEST_ASSETS_DIR:-/tmp/controller-tools/envtest}"
make envtest
export KUBEBUILDER_ASSETS="$ENVTEST_ASSETS_DIR"
go test ./api/...
go test ./bootstrap/...
go test ./cmd/...
go test ./controllers/...
go test ./controlplane/...
go test ./exp/...
