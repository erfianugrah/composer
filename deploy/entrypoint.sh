#!/bin/sh
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}
DOCKER_GID=${DOCKER_GID:-}

# Adjust composer user UID/GID
if [ "$(id -u composer)" != "$PUID" ]; then
  usermod -u "$PUID" composer 2>/dev/null || true
fi
if [ "$(id -g composer)" != "$PGID" ]; then
  groupmod -g "$PGID" composer 2>/dev/null || true
fi

# Docker socket group access
if [ -n "$DOCKER_GID" ]; then
  # Explicit GID provided
  if getent group "$DOCKER_GID" >/dev/null 2>&1; then
    DOCKER_GROUP=$(getent group "$DOCKER_GID" | cut -d: -f1)
  else
    addgroup -g "$DOCKER_GID" docker 2>/dev/null || true
    DOCKER_GROUP="docker"
  fi
  adduser composer "$DOCKER_GROUP" 2>/dev/null || true
elif [ -S /var/run/docker.sock ]; then
  # Auto-detect GID from socket
  SOCK_GID=$(stat -c '%g' /var/run/docker.sock)
  if [ "$SOCK_GID" != "0" ]; then
    if getent group "$SOCK_GID" >/dev/null 2>&1; then
      DOCKER_GROUP=$(getent group "$SOCK_GID" | cut -d: -f1)
    else
      addgroup -g "$SOCK_GID" docker 2>/dev/null || true
      DOCKER_GROUP="docker"
    fi
    adduser composer "$DOCKER_GROUP" 2>/dev/null || true
  fi
fi

# Fix ownership of data directories
chown -R composer:composer /opt/stacks /opt/composer /home/composer/.ssh 2>/dev/null || true

# Mark all stack directories as safe for git (PUID/PGID change causes dubious ownership)
su-exec composer git config --global --add safe.directory '*' 2>/dev/null || true

# Drop privileges and exec (su-exec without explicit group picks up supplementary groups from /etc/group)
exec su-exec composer "$@"
