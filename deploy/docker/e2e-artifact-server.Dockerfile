# syntax=docker/dockerfile:1.7
FROM nginxinc/nginx-unprivileged:1.29-alpine

COPY build/e2e-artifacts/ /usr/share/nginx/html/
COPY deploy/docker/e2e-artifact-server.conf /etc/nginx/conf.d/default.conf
