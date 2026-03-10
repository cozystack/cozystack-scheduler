REGISTRY ?= ghcr.io/cozystack/cozystack
TAG ?= $(shell git describe --tags --always --dirty)
IMAGE = $(REGISTRY)/cozystack-scheduler:$(TAG)
PUSH ?= 1
LOAD ?= 0
BUILDX_ARGS ?=

crd:
	controller-gen crd paths=./pkg/apis/... output:crd:dir=chart/crds

image:
	docker buildx build $(BUILDX_ARGS) \
		--tag $(IMAGE) \
		--label org.opencontainers.image.source=https://github.com/cozystack/cozystack-scheduler \
		--metadata-file metadata.json \
		$(if $(filter 1,$(PUSH)),--push) \
		$(if $(filter 1,$(LOAD)),--load) \
		.
	yq -r '.["containerimage.digest"]' metadata.json -o json | tr -d '"' > digest.txt
	echo "$(IMAGE)@$$(cat digest.txt)" > image-ref.txt
	sed -i "s|^image:.*|image: $$(cat image-ref.txt)|" chart/values.yaml
	rm -f metadata.json digest.txt image-ref.txt
