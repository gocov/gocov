#!/bin/bash
# EC2 user-data for the gocov host: Docker + compose plugin + repo checkout.
set -e
dnf install -y docker git
systemctl enable --now docker
mkdir -p /usr/local/lib/docker/cli-plugins
curl -fsSL https://github.com/docker/compose/releases/latest/download/docker-compose-linux-aarch64 \
  -o /usr/local/lib/docker/cli-plugins/docker-compose
chmod +x /usr/local/lib/docker/cli-plugins/docker-compose
usermod -aG docker ec2-user
git clone https://github.com/gocov/gocov /opt/gocov
