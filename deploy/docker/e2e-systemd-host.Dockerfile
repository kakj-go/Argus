# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.25.8-alpine AS generator

ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/argus-telemetry-e2e ./cmd/argus-telemetry-e2e
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags m4e2e -trimpath -ldflags "-s -w" \
      -o /out/argus-telemetry-e2e ./cmd/argus-telemetry-e2e

FROM ubuntu:24.04

ENV container=docker
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl iproute2 openssh-client openssh-server openssl sudo systemd systemd-sysv && \
    useradd --create-home --shell /bin/bash argus && \
    echo 'argus:M3-e2e-ssh-password' | chpasswd && \
	echo 'root:M3-e2e-ssh-password' | chpasswd && \
    mkdir -p /run/sshd && \
	mkdir -p /var/log/journal && \
	printf 'PasswordAuthentication yes\nPermitRootLogin yes\n' >/etc/ssh/sshd_config.d/argus-e2e.conf && \
    systemctl enable ssh.service && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

COPY --from=generator /out/argus-telemetry-e2e /usr/local/bin/argus-telemetry-e2e

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
