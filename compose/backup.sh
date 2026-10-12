#!/bin/sh
set -eu
umask 077

compose_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
compose_cli="$compose_dir/compose.sh"
destination=${1:-"$compose_dir/../backups/tabletop-$(date -u +%Y%m%dT%H%M%SZ).tar.gz"}
if [ "$#" -gt 1 ] || [ -e "$destination" ]; then
    printf 'Usage: %s [new-backup.tar.gz]; destination must not already exist.\n' "$0" >&2
    exit 1
fi
container_id=$("$compose_cli" ps -a -q tabletop)
if [ -z "$container_id" ]; then
    printf 'No tabletop container exists to back up. Start the configured service first.\n' >&2
    exit 1
fi
mkdir -p -- "$(dirname -- "$destination")"
temporary=$(mktemp "$(dirname -- "$destination")/.tabletop-backup.XXXXXX")
paused_here=false
cleanup() {
    if [ "$paused_here" = true ]; then "$compose_cli" unpause tabletop >/dev/null; fi
    if [ -n "$temporary" ]; then rm -f -- "$temporary"; fi
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
state=$(docker inspect --format '{{.State.Running}} {{.State.Paused}}' "$container_id")
if [ "$state" = 'true false' ]; then
    "$compose_cli" pause tabletop >/dev/null
    paused_here=true
fi
"$compose_cli" run --rm -T --no-deps maintenance tar -czf - -C /data . > "$temporary"
gzip -t "$temporary"
mv -- "$temporary" "$destination"
temporary=''
printf 'Saved backup: %s\n' "$destination"
