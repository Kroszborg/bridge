#!/usr/bin/env bash
# Deploys Bridge to a single server over SSH: builds the images here, ships
# them with `docker save | docker load`, and starts the production stack.
# The server never builds anything, so a small (2 GB) instance is enough.
#
#   tools/deploy.sh ubuntu@203.0.113.10 ~/.ssh/lightsail.pem [.env.production]
#
# Images are tagged with BRIDGE_VERSION: from the environment, else the env
# file, else the version in packages/sdk/package.json (the release version).
#
# The env file holds the production settings (see docs/self-hosting/lightsail.md)
# and is copied to the server as /opt/bridge/.env.
set -euo pipefail

target=${1:?usage: tools/deploy.sh user@host key.pem [env-file]}
key=${2:?usage: tools/deploy.sh user@host key.pem [env-file]}
envfile=${3:-.env.production}
dir=/opt/bridge
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

[[ -f $envfile ]] || { echo "missing $envfile" >&2; exit 1; }
version=${BRIDGE_VERSION:-$(sed -n 's/^BRIDGE_VERSION=//p' "$envfile" | tail -n 1)}
version=${version:-$(node -p "require('./packages/sdk/package.json').version")}
export BRIDGE_VERSION=$version
ssh_=(ssh -i "$key" -o BatchMode=yes "$target")
compose=(docker compose --env-file "$envfile" -f docker-compose.yml -f docker-compose.prod.yml)

echo "==> Building images ($version)"
"${compose[@]}" build api dashboard web

images=$("${compose[@]}" config --images | grep -v '^caddy' | sort -u)
echo "==> Shipping $(echo "$images" | wc -l | tr -d ' ') images to $target"
# shellcheck disable=SC2086
docker save $images | gzip -1 | "${ssh_[@]}" 'gunzip | docker load'

echo "==> Copying configuration"
"${ssh_[@]}" "mkdir -p $dir/docker"
scp -q -i "$key" -o BatchMode=yes docker-compose.yml docker-compose.prod.yml "$target:$dir/"
scp -q -i "$key" -o BatchMode=yes docker/Caddyfile "$target:$dir/docker/Caddyfile"
scp -q -i "$key" -o BatchMode=yes "$envfile" "$target:$dir/.env"
"${ssh_[@]}" "chmod 600 $dir/.env"

echo "==> Starting"
"${ssh_[@]}" "cd $dir && export BRIDGE_VERSION=$version && docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --no-build --remove-orphans && docker image prune -f >/dev/null && docker compose -f docker-compose.yml -f docker-compose.prod.yml ps --format '{{.Name}}\t{{.Status}}'"
