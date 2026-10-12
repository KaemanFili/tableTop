#!/bin/sh
set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

for command in go docker; do
    if ! command -v "$command" >/dev/null 2>&1; then
        printf '%s is required to rebuild tableTop.\n' "$command" >&2
        exit 1
    fi
done

if ! docker info >/dev/null 2>&1; then
    printf 'Start Docker and ensure this user can access its daemon.\n' >&2
    exit 1
fi

cd "$project_dir"
printf 'Building local executable…\n'
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o main .

printf 'Building Docker image…\n'
DOCKER_BUILDKIT=0 docker build -f compose/Dockerfile -t tabletop-tabletop .

printf 'Built main and the tabletop image.\n'
