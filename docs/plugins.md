# Content plugins

Each tabletop selects one installed plugin version. Its maps, units, statuses,
and artwork remain read-only in the installed folder. MongoDB or JSON stores
only tabletop additions and copies. New sessions perform no default imports.
The built-in version supplies Knight, Goblin, and the standard conditions.

The included [D&D 5e Monsters plugin](../plugins/dnd-5e/README.md) supplies all
3,617 entries from the October 13, 2026 D&D Beyond catalog, with 2,760 individual
portraits and eleven default environment maps. Select **D&D 5e Monsters v1** when
creating a session. Entries without catalog artwork use the default NPC image.
Full combat stat blocks are not part of the token format.

## Folder layout

```text
plugins/fantasy/1/
  plugin.json
  maps.json
  units.json
  statuses.json
  maps/forest.png
  boards/forest.json
  units/scout.png
```

```json
{
  "schemaVersion": 2,
  "id": "fantasy",
  "version": 1,
  "name": "Fantasy",
  "description": "A fantasy adventure.",
  "maps": "maps.json",
  "units": "units.json",
  "statuses": "statuses.json"
}
```

`maps.json` is a metadata array; each entry has a unique `key`, name, image path,
grid width/height/offsets, and optional `boardFile` referring to a SavedBoard JSON
object. An omitted board starts empty. Board files are loaded when selected;
map listings and thumbnails do not load saved boards.

```json
[
  {
    "key": "forest",
    "name": "Forest",
    "image": "maps/forest.png",
    "width": 20,
    "height": 20,
    "offsetX": 0,
    "offsetY": 0,
    "boardFile": "boards/forest.json"
  }
]
```

`units.json` is an array of unique keys, names, `pc`/`npc` type, optional size,
image framing, and image path. Defaults are Medium and centered framing. An
omitted image uses the built-in PC/NPC artwork. In default board files, token
`imageID` can reference a unit key from that version. `statuses.json` contains
ordered `{ "value": "Poisoned", "color": "#65A30D" }` definitions.

Use only contained relative paths. PNG, JPEG, and GIF are accepted within the
existing 4096 × 4096 image limit and 10 MiB map / 5 MiB unit byte limits. Responses
normalize plugin artwork to PNG without writing copies. Default content files
are bounded by `TABLETOP_PLUGIN_CONTENT_LIMIT_BYTES` (16 MiB by default), and the
manifest by `TABLETOP_PLUGIN_METADATA_LIMIT_BYTES` (1 MiB). There are no map/unit
count caps; existing board and status limits apply.

## Installation and retention

Install a complete version directory before exposing it to the app. The manifest
identity must match its folders. Different versions coexist; duplicate exact
ID/version installations are unavailable. Discovery refreshes with the lobby.
Decoded packs are cached in memory (up to 32 versions). Discovery still validates
file identity before reuse; changed or removed files invalidate cached content.
For a possible shared Redis cache when running separate session processes, see
[proposed session workers](session-workers.md).
Never change a version's content after it has been used: tabletops pin its
fingerprint. Publish changed content under a new version instead.

The app reads `plugins/` beneath its asset root. `TABLETOP_PLUGIN_DIRS` adds
additional roots separated by the platform path-list separator (`:` on Linux).
For Compose, add a read-only mount through an override file:

```yaml
services:
  tabletop:
    environment:
      TABLETOP_PLUGIN_DIRS: /app/operator-plugins
    volumes:
      - /srv/tabletop/plugins:/app/operator-plugins:ro
```

Use that override together with the existing Compose file. Keep the container's
bundled `/app/plugins` directory available. Retain older referenced bundled
versions when upgrading images, mounting them through an additional root when
needed. Avoid installing duplicate copies of the same ID/version.

Retain every exact version referenced by a tabletop or an owned document's
artwork reference. Removing or changing a required folder blocks dependent
content access; restoring the original files recovers it. The app does not
install, copy, upgrade, or delete plugin folders. Installed folders are outside
the writable data directory's quotas; manage their disk capacity on the host.

## Copies, saves, and backups

Users cannot delete defaults. Create copy gives a map or unit an owned ID while
retaining its plugin artwork reference. Unit copy forms allow changing the name,
type, size, framing, and optionally uploading replacement artwork. Saving a board
using a default map creates an owned map and switches the active map after a
successful save. Later saves update that copy. Defaults remain available.

The status additions document uses schema version 2 and starts empty. Effective
statuses combine plugin defaults with additions; additions cannot replace default
names or exceed the combined 256-definition limit.

Back up application data, MongoDB when enabled, and all exact referenced plugin
folders together. External plugin mounts are not included in data-only backup
scripts. Restore these folders before serving dependent tabletops. Old imported
session data and old pending-import recovery are not migrated by this change;
start with new tabletops. No existing data is deleted automatically.
