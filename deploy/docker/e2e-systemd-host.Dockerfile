# syntax=docker/dockerfile:1.7
FROM ubuntu:24.04

ENV container=docker
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates openssh-server systemd systemd-sysv && \
    useradd --create-home --shell /bin/bash argus && \
    echo 'argus:M3-e2e-ssh-password' | chpasswd && \
	echo 'root:M3-e2e-ssh-password' | chpasswd && \
    mkdir -p /run/sshd && \
	mkdir -p /var/log/journal && \
	printf 'PasswordAuthentication yes\nPermitRootLogin yes\n' >/etc/ssh/sshd_config.d/argus-e2e.conf && \
    systemctl enable ssh.service && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]
