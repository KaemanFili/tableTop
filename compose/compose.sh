#!/bin/sh
set -eu

compose_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
dev=false
tunnel=false
host_data=${TABLETOP_HOST_DATA:-0}
while [ "$#" -gt 0 ]; do
    case "$1" in
        -dev) dev=true; shift ;;
        -tunnel) tunnel=true; shift ;;
        -host-data) host_data=1; shift ;;
        *) break ;;
    esac
done
if [ "$dev" = true ] && [ "$tunnel" = true ]; then
    printf 'Do not expose the unauthenticated development board through a public tunnel.\n' >&2
    exit 1
fi
if [ "$dev" = true ] && [ "$host_data" = 1 ]; then
    printf 'Development mode uses its own volume; do not combine it with host-data mode.\n' >&2
    exit 1
fi
if [ "$tunnel" = true ]; then
    if [ "$host_data" = 1 ]; then
        exec docker compose -f "$compose_dir/compose.yaml" -f "$compose_dir/compose.host-data.yaml" -f "$compose_dir/compose.tunnel.yaml" "$@"
    fi
    exec docker compose -f "$compose_dir/compose.yaml" -f "$compose_dir/compose.tunnel.yaml" "$@"
fi
if [ "$dev" = true ]; then
    exec docker compose -f "$compose_dir/compose.yaml" -f "$compose_dir/compose.dev.yaml" "$@"
fi
if [ "$host_data" = 1 ]; then
    exec docker compose -f "$compose_dir/compose.yaml" -f "$compose_dir/compose.host-data.yaml" "$@"
fi
exec docker compose -f "$compose_dir/compose.yaml" "$@"
