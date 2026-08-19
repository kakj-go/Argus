# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.25.3-alpine AS build

ARG TARGETOS=linux
ARG TARGETARCH=arm64
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY build/otelcol ./build/otelcol
RUN go run go.opentelemetry.io/collector/cmd/builder@v0.133.0 \
    --skip-compilation --config build/otelcol/builder-linux-arm64.yaml && \
    cd build/otelcol/dist/linux-arm64 && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o argus-otelcol .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /src/build/otelcol/dist/linux-arm64/argus-otelcol /usr/local/bin/argus-otelcol
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/argus-otelcol"]
