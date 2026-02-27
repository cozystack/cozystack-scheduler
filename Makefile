REGISTRY ?= ghcr.io/cozystack/cozystack
TAG ?= $(shell git describe --tags --always --dirty)
IMAGE = $(REGISTRY)/cozystack-scheduler:$(TAG)
BUILDX_ARGS ?=

image:
	docker buildx build $(BUILDX_ARGS) \
		--tag $(IMAGE) \
		--metadata-file metadata.json \
		.
	yq -r '.["containerimage.digest"]' metadata.json -o json | tr -d '"' > digest.txt
	echo "$(IMAGE)@$$(cat digest.txt)" > image-ref.txt
	sed -i "s|^image:.*|image: $$(cat image-ref.txt)|" chart/values.yaml
	rm -f metadata.json digest.txt image-ref.txt
