IMAGE ?= ghcr.io/bi119ate5hxk/atomcam-rotctld:latest
LOCAL_IMAGE ?= atomcam-rotctld:local

.PHONY: build test test-race fmt docker-build docker-build-armv7 package

build:
	go build -o atomcam-rotctld ./cmd/atomcam-rotctld

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w cmd internal

docker-build:
	docker build -t $(LOCAL_IMAGE) .

docker-build-armv7:
	docker buildx build --platform linux/arm/v7 --load -t $(IMAGE) .

package:
	mkdir -p dist
	tar --exclude='./dist' --exclude='./.git' --exclude='./coverage.out' \
		-czf dist/atomcam-rotctld-src.tar.gz .
