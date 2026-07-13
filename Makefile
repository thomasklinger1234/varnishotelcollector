include ./Makefile.Common

VERSION ?= nightly

ALL_MODULES := $(shell find . -mindepth 2 \
				-type f \
				-name "go.mod" \
				-not -path "." \
				-not -path "./build/*" \
				-exec dirname {} \; | sort )

GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)
GOMODULES = $(ALL_MODULES) $(PWD)

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
