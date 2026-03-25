#!/bin/bash

set -o errexit
set -o pipefail

echo "Running unit-tests.sh"

# Ensure that some home var is set and that it's not the root
export HOME=${HOME:=/tmp/kubebuilder/testing}
if [ $HOME == "/" ]; then
  export HOME=/tmp/kubebuilder/testing
fi

export KUBEBUILDER_ENVTEST_KUBERNETES_VERSION=1.22.0

make setup-envtest
export KUBEBUILDER_ASSETS=$(./hack/tools/bin/setup-envtest use --use-env -p path $KUBEBUILDER_ENVTEST_KUBERNETES_VERSION)
go test ./api/...
go test ./bootstrap/...
go test ./cmd/...
go test ./controllers/...
go test ./controlplane/...
go test ./exp/...
