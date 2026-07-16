.PHONY: build test test-race fmt docker-build docker-build-armv7

build:
	go build -o atomcam-rotctld ./cmd/atomcam-rotctld

test:
	go test ./...

test-race:
	go test -race ./...

fmt:
	gofmt -w cmd internal

docker-build:
	docker build -t atomcam-rotctld:local .

docker-build-armv7:
	docker buildx build --platform linux/arm/v7 --load -t atomcam-rotctld:armv7 .
