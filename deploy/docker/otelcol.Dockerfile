# The locked OCB distribution is built by `make otelcol-linux-arm64` (or
# otelcol-linux-amd64) before this image is packaged. Rebuilding it here
# duplicates the toolchain and can consume tens of GiB of Docker build cache
# during Kubernetes E2E runs. DIST_PATH selects the arch-specific dist
# directory; the multi-arch publish flow overrides it per platform.
FROM gcr.io/distroless/static-debian12:nonroot
ARG DIST_PATH=build/otelcol/dist/linux-arm64
COPY ${DIST_PATH}/argus-otelcol /usr/local/bin/argus-otelcol
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/argus-otelcol"]
