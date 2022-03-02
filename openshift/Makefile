BIN_DIR := bin
TOOLS_DIR := hack/tools
TOOLS_BIN_DIR := $(TOOLS_DIR)/$(BIN_DIR)
KUSTOMIZE := ../$(TOOLS_BIN_DIR)/kustomize

$(RELEASE_DIR):
	mkdir -p $(RELEASE_DIR)/

$(KUSTOMIZE): # Download kustomize using hack script into tools folder.
	../hack/ensure-kustomize.sh

.PHONY: ocp-manifests
ocp-manifests: $(RELEASE_DIR) $(KUSTOMIZE) ## Builds openshift specific manifests
	# Build core-components.
	$(KUSTOMIZE) build ../config/default > core-components.yaml
