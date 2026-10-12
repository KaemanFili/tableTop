# Tabletop content plugins

Status: proposed. This document specifies a future plugin feature; session
creation does not select plugins yet.

## Purpose and scope

A plugin supplies default maps, reusable units, and status definitions for a new
tabletop. The creator selects one plugin when creating the session. Each session
owns its copied content and can change or delete it independently afterward.
The first version supports one content plugin per session. Combining plugins,
executable hooks, automatic upgrades, and a plugin marketplace are future work.

## Project layout

```text
plugins/
  fantasy/
    plugin.json
    maps/forest.png
    units/goblin.png
internal/plugins/          # Discovery, validation, and initialization code
```

Bundled plugins ship with the app. Operators can mount additional plugin
folders read-only under `/app/plugins/`. Runtime copies of artwork remain under
`data/sessions/{tabletopID}/`, alongside existing session artwork. Plugin
manifests and original assets remain separate from mutable tabletop data.

The existing Knight, Goblin, and built-in status definitions become a built-in
plugin. Its map list may be empty. A fresh installation always offers it.

## Manifest

```json
{
  "schemaVersion": 1,
  "id": "fantasy",
  "version": 1,
  "name": "Fantasy",
  "description": "Maps, creatures, and conditions for a fantasy adventure.",
  "maps": [
    {
      "key": "forest",
      "name": "Forest",
      "image": "maps/forest.png",
      "width": 20,
      "height": 20,
      "offsetX": 0,
      "offsetY": 0
    }
  ],
  "units": [
    {
      "key": "goblin",
      "name": "Goblin",
      "unitType": "npc",
      "image": "units/goblin.png"
    }
  ],
  "statuses": [
    { "value": "Poisoned", "color": "#65A30D" }
  ]
}
```

- `schemaVersion` identifies the manifest format; reject unsupported versions.
- `id` is a stable lowercase slug. `version` is a positive integer identifying
  a released pack. Display the human-readable name and description in the picker.
- Map and unit keys are unique within their respective lists.
- Maps use the existing grid dimensions and offsets. Initial saved board content
  is optional, uses the current embedded saved-board format, and must satisfy
  board limits. An absent board starts empty when the map is loaded.
- Units support the existing name, PC/NPC type, size, and image framing fields.
  Omitted size and framing use the existing defaults. Omitted unit artwork uses
  the existing PC/NPC artwork. Map artwork is required.
- Statuses are an ordered list of 1–256 unique names with valid `#RRGGBB` colors.
  Names and image metadata follow existing application validation.
- Asset paths resolve inside the plugin folder; reject absolute paths, traversal,
  and symlinks escaping that folder. Apply existing image format, dimension,
  upload-byte, and normalized-artwork storage limits.
- Bound manifest size and entry counts, and check each generated MongoDB document
  against the 16 MiB limit before initializing a tabletop.

Invalid plugins are reported to the operator and omitted from the picker. A
selected plugin that disappears or fails validation must produce a creation
error rather than silently loading different content. Duplicate plugin IDs
make both conflicting installations unavailable until the conflict is fixed.

## Session creation

The create-session form adds a required Plugin selector. Show the available
plugins and their descriptions; select the built-in plugin initially. The server
validates the submitted plugin ID against its discovered plugins.

Persist the chosen plugin ID and version in the tabletop definition. Before the
new tabletop is available to other users:

1. Validate the complete plugin and preflight storage/document limits.
2. Create the tabletop in an initializing state.
3. Copy and normalize artwork into that tabletop's data directory.
4. Write its map, unit, and status documents using the tabletop ID as scope.
5. Mark initialization complete and open the board.

Initialization must be idempotent and recoverable after interruption. Failures
must leave a clear retry or cleanup path; an incomplete tabletop cannot serve a
partially initialized library. Never treat an intentionally empty map/unit
library as a reason to reinstall plugin defaults.

## Document identity and ownership

Derive stable internal IDs from plugin ID, content kind, and entry key. Map IDs
and custom unit/image IDs must remain compatible with the current 32-character
hexadecimal identifiers. Include tabletop scope in MongoDB document identity.
Store plugin provenance separately from editable names and other content fields.

Seed defaults once. Restarting the app or changing plugin files preserves the
session's saved documents, including deleted defaults. Existing sessions remain
usable if their source plugin is removed. Plugin upgrades require an explicit,
separately designed user action and never overwrite session content automatically.

## Status isolation

Replace the current server-wide status source with a status document per
session, using settings document ID `unit_statuses/{tabletopID}`. The session's
list drives its picker, accepted status values, and token colors. Server-side
rendering and status updates must use the session's definitions rather than a
global status list.

Assigned token statuses remain embedded in saved maps. Save Map preserves them;
Load Map restores them. Live boards continue to reset on restart. Removing a
status definition preserves the saved value, which displays without a colored
border until restored or changed.

Existing sessions retain their libraries. Initialize their session status lists
from the existing server-wide status document once; preserve any custom names
and colors. Assign legacy sessions built-in provenance without reseeding maps
or units. Provide equivalent persisted session defaults when MongoDB is disabled.

## Deletion and backups

Deleting a tabletop removes its copied artwork, scoped map/unit documents,
session status document, and initialization markers. Installed plugin files and
other tabletops remain available. Interrupted deletion must be retryable.
Backups must include both session files and database documents. Restore a matched
pair before serving any session; source plugins need not be installed to load
already copied session content.

## Acceptance checks

- Two sessions using different plugins receive different starting libraries and
  status pickers, with no cross-session changes.
- A session retains custom entries and deleted defaults across restart.
- Editing or removing a source plugin leaves existing sessions usable.
- Invalid assets, manifests, duplicate IDs, and oversized documents produce clear
  errors without exposing a partially initialized session.
- Interrupted initialization resumes without duplicate documents or artwork.
- Existing saved maps and status definitions survive the migration unchanged.
- Assigned statuses survive Save Map and Load Map with session-specific colors.
- Deleting a session removes only its copied content and scoped documents.
- A matched file/database restore works without reinstalling source plugins.
