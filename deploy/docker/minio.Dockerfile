# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.24.8-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG MINIO_VERSION=RELEASE.2025-10-15T17-29-55Z

RUN apk add --no-cache ca-certificates git
WORKDIR /src
RUN git clone --depth 1 --branch "$MINIO_VERSION" https://github.com/minio/minio.git .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -tags kqueue -ldflags "-s -w" -o /out/minio .

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S minio && adduser -S -G minio minio
COPY --from=build /out/minio /usr/local/bin/minio
USER minio:minio
VOLUME ["/data"]
EXPOSE 9000 9001
ENTRYPOINT ["/usr/local/bin/minio"]
CMD ["server", "/data", "--console-address", ":9001"]
