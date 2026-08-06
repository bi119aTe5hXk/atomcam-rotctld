# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/atomcam-rotctld ./cmd/atomcam-rotctld

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/atomcam-rotctld /usr/local/bin/atomcam-rotctld

USER 65532:65532
EXPOSE 4533
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD ["atomcam-rotctld", "healthcheck"]
ENTRYPOINT ["atomcam-rotctld"]
