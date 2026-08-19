# syntax=docker/dockerfile:1.7
FROM nginxinc/nginx-unprivileged:1.29-alpine

COPY build/otelcol/artifacts/argus-otelcol-linux-arm64.tar.gz /usr/share/nginx/html/m7/linux-arm64.tar.gz
COPY deploy/docker/e2e-artifact-server.conf /etc/nginx/conf.d/default.conf

