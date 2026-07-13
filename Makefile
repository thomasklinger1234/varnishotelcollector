include ./Makefile.Common

VERSION ?= nightly

ALL_MODULES := $(shell find . -mindepth 2 \
				-type f \
				-name "go.mod" \
				-not -path "./build/*" \
				-exec dirname {} \; | sort )

GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
GOMODULES = $(ALL_MODULES)

OTELCOL_BUILDER ?= $(GOCMD) run go.opentelemetry.io/collector/cmd/builder
OTELCOL_BUILDER_CONFIG ?= distribution/varnishotelcollector/manifest.yaml
OTELCOL_BUILDER_OUT ?= build
OTELCOL_BUILDER_ARGS ?=

# Define a delegation target for each module
.PHONY: $(GOMODULES)
$(GOMODULES):
	@echo "Running target '$(TARGET)' in module '$@'"
	$(MAKE) -C $@ $(TARGET)

# Triggers each module's delegation target
.PHONY: for-all-target
for-all-target: $(GOMODULES)

all-modules:
	@echo $(ALL_MODULES) | tr ' ' '\n' | sort

.PHONY: gomoddownload
gomoddownload:
	$(MAKE) $(FOR_GROUP_TARGET) TARGET="moddownload"

.PHONY: gotest
gotest:
	@$(MAKE) for-all-target TARGET="test"

.PHONY: gotidy
gotidy:
	@$(MAKE) for-all-target TARGET="tidy"

.PHONY: gofmt
gofmt:
	@$(MAKE) for-all-target TARGET="fmt"

.PHONY: gogenerate
gogenerate:
	@$(MAKE) for-all-target TARGET="generate"
	$(MAKE) fmt


.PHONY: distribution-pre
distribution-pre:
	$(OTELCOL_BUILDER) --config $(OTELCOL_BUILDER_CONFIG) --skip-compilation $(OTELCOL_BUILDER_ARGS)
	$(GOCMD) work edit -use=$(OTELCOL_BUILDER_OUT)

.PHONY: distribution-post
distribution-post:
	$(GOCMD) work edit -dropuse=$(OTELCOL_BUILDER_OUT)
	mkdir -p dist/varnishotelcollector_$(GOOS)_$(GOARCH)
	cp $(OTELCOL_BUILDER_OUT)/varnishotelcollector dist/varnishotelcollector_$(GOOS)_$(GOARCH)/varnishotelcollector_$(GOOS)_$(GOARCH)

.PHONY: distribution
distribution:
	$(MAKE) clean
	$(MAKE) distribution-pre
	$(OTELCOL_BUILDER) --config $(OTELCOL_BUILDER_CONFIG) $(OTELCOL_BUILDER_ARGS)
	$(MAKE) distribution-post

distribution-release:
	$(MAKE) -C distribution/varnishotelcollector docker-push VERSION=$(VERSION) VARNISH_VERSION=8
	$(MAKE) -C distribution/varnishotelcollector docker-push VERSION=$(VERSION) VARNISH_VERSION=9

clean:
	$(RM) -r $(OTELCOL_BUILDER_OUT)
	$(RM) -r dist
	go work edit -dropuse=$(OTELCOL_BUILDER_OUT)