#!/bin/sh
set -eu

if [ "$#" -ne 1 ] || [ ! -f "$1" ]; then
    printf 'Usage: %s backup.tar.gz\nRestore requires an empty target volume and a stopped app.\n' "$0" >&2
    exit 1
fi
compose_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
compose_cli="$compose_dir/compose.sh"
if [ -n "$("$compose_cli" ps --status running -q tabletop)" ]; then
    printf 'Stop tabletop before restoring. Save any live board state first.\n' >&2
    exit 1
fi
gzip -t "$1"
python3 - "$1" <<'PY'
import sys
import tarfile
from pathlib import PurePosixPath

with tarfile.open(sys.argv[1], "r:gz") as archive:
    for entry in archive:
        path = PurePosixPath(entry.name)
        if path.is_absolute() or ".." in path.parts or not (entry.isfile() or entry.isdir()):
            raise SystemExit("Backup contains an unsafe path, link, or special file; restore refused.")
PY
# Populate a newly selected volume with /app/data's non-root ownership before
# the maintenance container mounts it. This creates, but does not start, the app.
"$compose_cli" create --no-build tabletop >/dev/null
# Only restore into an empty volume; the original volume is never erased.
"$compose_cli" run --rm -T --no-deps maintenance sh -eu -c '
    if [ -n "$(ls -A /data)" ]; then
        printf "Restore destination is not empty. Set TABLETOP_DATA_VOLUME to a new volume name.\n" >&2
        exit 1
    fi
    tar -xzf - -C /data
' < "$1"
printf 'Backup restored to the configured data volume. Start tabletop to use it.\n'
