TOOLS_DIR := tools

include provider-version.mk

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

MANIFESTS_GEN := $(TOOLS_DIR)/bin/manifests-gen
KUSTOMIZE := $(TOOLS_DIR)/bin/kustomize

$(TOOLS_DIR)/bin/%:
	$(MAKE) -C $(TOOLS_DIR) bin/$*

.PHONY: verify
verify: verify-ocp-manifests

.PHONY: verify-%
verify-%: ## Ensure no diff after running some other target
	$(MAKE) $*
	./verify-diff.sh

.PHONY: update-manifests-gen
update-manifests-gen:
	cd tools && go get github.com/openshift/cluster-capi-operator/manifests-gen && go mod tidy && go mod vendor

.PHONY: update-ipam-ref
update-ipam-ref:
	# Get the current HEAD of the release-4.20 branch
	$(eval current_head := $(shell git ls-remote https://github.com/openshift/cluster-api release-4.20 | awk '$$2 == "refs/heads/release-4.20" {print $$1}'))

	# Pin the current head in the ipam CRD kustomize resource target
	sed -i "s,https://github.com/openshift/cluster-api/config/crd?ref=.*,https://github.com/openshift/cluster-api/config/crd?ref=$(current_head)," ipam/kustomization.yaml

.PHONY: ipam-manifests
ipam-manifests: $(KUSTOMIZE)
	$(KUSTOMIZE) build ipam -o manifests/0000_30_cluster-api_04_crd.core-cluster-api.yaml

# Rebasebot runs ocp-manifests, so we make it generate ipam-manifests too
.PHONY: ocp-manifests
ocp-manifests: ipam-manifests $(MANIFESTS_GEN) | $(RELEASE_DIR) ## Builds openshift specific manifests
	# Generate provider manifests.
	# TODO: load the provider-version dynamically at rebase time when this is invoked by the Rebase Bot during one of its lifecycle hooks.
	$(MANIFESTS_GEN) --provider-name "cluster-api" --provider-type "CoreProvider" --provider-version "${PROVIDER_VERSION}" --base-path "../" --manifests-path "./manifests" --kustomize-dir="openshift"
