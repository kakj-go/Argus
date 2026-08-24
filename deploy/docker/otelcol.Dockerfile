# The locked OCB distribution is built by `make otelcol-linux-arm64` before
# this image is packaged. Rebuilding it here duplicates the toolchain and can
# consume tens of GiB of Docker build cache during Kubernetes E2E runs.
FROM gcr.io/distroless/static-debian12:nonroot
COPY build/otelcol/dist/linux-arm64/argus-otelcol /usr/local/bin/argus-otelcol
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/argus-otelcol"]
