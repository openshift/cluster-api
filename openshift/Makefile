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

.PHONY: ipam-manifests
ipam-manifests: $(KUSTOMIZE)
	$(KUSTOMIZE) build ipam -o manifests/0000_30_cluster-api_04_crd.core-cluster-api.yaml

# Rebasebot runs ocp-manifests, so we make it generate ipam-manifests too
.PHONY: ocp-manifests
ocp-manifests: ipam-manifests $(MANIFESTS_GEN) | $(RELEASE_DIR) ## Builds openshift specific manifests
	# Generate provider manifests.
	$(MANIFESTS_GEN) -manifests-path "./capi-operator-manifests" -kustomize-dir="../openshift" \
		-name "cluster-api" \
		-attribute type="core" \
		-install-order=10 \
		-attribute version="${PROVIDER_VERSION}" \
		-self-image-ref registry.ci.openshift.org/openshift:cluster-capi-controllers \
		-protect-cluster-resource cluster \
		-manifests-summary
