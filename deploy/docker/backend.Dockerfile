# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal

RUN mkdir -p /out && \
    for name in argus-server argus-worker argus-connector-gateway argus-telemetry argus-connector argusctl; do \
      CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
        -trimpath \
        -ldflags "-s -w -X github.com/kakj-go/Argus/internal/buildinfo.Version=$VERSION -X github.com/kakj-go/Argus/internal/buildinfo.Commit=$COMMIT -X github.com/kakj-go/Argus/internal/buildinfo.Date=$BUILD_DATE" \
        -o /out/$name ./cmd/$name; \
    done

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /usr/local/bin/
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/argus-server"]
