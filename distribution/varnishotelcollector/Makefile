SRC_ROOT := $(shell git rev-parse --show-toplevel)

VERSION ?= latest
VARNISH_VERSION ?= latest

IMAGE_NAME ?= varnishotelcollector/varnishotelcollector
IMAGE_TAG ?= $(VERSION)
IMAGE_REGISTRY ?= ghcr.io/thomasklinger1234

DOCKER_BUILD_ARGS := \
	--progress=plain \
	--build-arg VARNISH_VERSION=$(VARNISH_VERSION) \
	--label=org.opencontainers.image.created="$(shell date)" \
	--label=org.opencontainers.image.name=$(IMAGE_NAME) \
	--label=org.opencontainers.image.revision=$(git rev-parse --short HEAD) \
	--label=org.opencontainers.image.version=$(VERSION) \
	--label=org.opencontainers.image.source=https://github.com/thomasklinger1234/varnishotelcollector \
	--label=org.opencontainers.image.licenses=Apache-2.0

docker/linux/amd64:
	docker buildx build $(DOCKER_BUILD_ARGS) \
		--rm \
		--pull \
		--tag $(IMAGE_NAME) \
		--platform linux/amd64 \
		--file $(SRC_ROOT)/distribution/varnishotelcollector/Dockerfile \
		$(SRC_ROOT)

docker/linux/arm64:
	docker buildx build $(DOCKER_BUILD_ARGS) \
		--rm \
		--pull \
		--tag $(IMAGE_NAME) \
		--platform linux/arm64 \
		--file $(SRC_ROOT)/distribution/varnishotelcollector/Dockerfile \
		$(SRC_ROOT)

docker: docker/linux/amd64 docker/linux/arm64

docker-tag:
	docker tag $(IMAGE_NAME) $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)-varnish$(VARNISH_VERSION)

docker-push: docker docker-tag
	docker push $(IMAGE_REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)-varnish$(VARNISH_VERSION)
