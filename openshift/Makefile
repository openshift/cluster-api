BIN_DIR := bin
TOOLS_DIR := tools

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

MANIFESTS_GEN := go run ./vendor/github.com/openshift/cluster-capi-operator/manifests-gen/

.PHONY: check-env
check-env:
ifndef PROVIDER_VERSION
	$(error PROVIDER_VERSION is undefined)
endif

.PHONY: update-manifests-gen
update-manifests-gen:
	cd tools && go get github.com/openshift/cluster-capi-operator/manifests-gen && go mod tidy && go mod vendor

.PHONY: ocp-manifests
ocp-manifests: $(RELEASE_DIR) check-env ## Builds openshift specific manifests
	# Generate provider manifests.
	# TODO: load the provider-version dynamically at rebase time when this is invoked by the Rebase Bot during one of its lifecycle hooks.
	cd tools && $(MANIFESTS_GEN) --provider-name "cluster-api" --provider-type "CoreProvider" --provider-version "${PROVIDER_VERSION}" --base-path "../../" --manifests-path "../manifests" --kustomize-dir="openshift"
