# MongoDB storage

Set `TABLETOP_MONGODB_URI` to enable MongoDB for map and unit libraries and
status definitions. `TABLETOP_MONGODB_DATABASE` defaults to `tabletop`. Without
a URI, owned libraries and status additions use JSON files. Plugin defaults are
read from installed version folders in both modes.

For a host process connected to a local MongoDB server:

```sh
TABLETOP_MONGODB_URI=mongodb://127.0.0.1:27017 \
TABLETOP_MONGODB_DATABASE=tabletop go run . -dev
```

Compose includes a `mongo:8.0` service and connects the app to
`mongodb://mongo:27017` by default. Start both with:

```sh
./compose/compose.sh up --build -d
```

The app waits for MongoDB's health check before starting. MongoDB uses an internal
Docker network shared with the app and publishes no host port. Its data and
configuration directories use persistent named volumes. Development mode uses
separate MongoDB volumes and database `tabletop_dev`; set
`TABLETOP_DEV_MONGODB_DATABASE` to override that database name.

To use an external server, set `TABLETOP_MONGODB_URI` in `compose/.env`. For host
processes, MongoDB remains optional and is enabled by setting the URI yourself.
`TABLETOP_MONGODB_DATA_VOLUME` and `TABLETOP_MONGODB_CONFIG_VOLUME` override the
normal Compose volume names. Stopping Compose retains these volumes.

Use one database per deployment/data directory. The app shares one connection
pool, requires acknowledged writes, and bounds operation timeouts. A database
failure is reported rather than silently switching back to JSON.

## Map and unit documents

The `maps` and `units` collections contain one document per definition:

```json
{
  "_id": "TABLETOP_ID/MAP_ID",
  "scope": "TABLETOP_ID",
  "schemaVersion": 1,
  "definition": {
    "id": "MAP_ID",
    "name": "Forest",
    "width": 20,
    "height": 10,
    "offsetX": 0,
    "offsetY": 0,
    "board": {
      "units": [],
      "strokes": [],
      "fog": [],
      "rulers": []
    }
  }
}
```

The development board uses scope `dev`; authenticated boards use their tabletop
ID. Scope indexes support loading one tabletop's libraries. Document IDs include
the scope so identical definition IDs cannot collide between tabletops.

Save Map replaces the map document, committing metadata and the embedded saved
board together. Assigned token statuses are included in `board.units`. Live board
state resets on restart; Load Map restores saved content. Shutdown behavior is
unchanged. Existing boards over the application capacity limits remain listed
and deletable; attempting to load or save them is refused.

If a delete response is lost, the app checks MongoDB to confirm whether the
document was removed. While the outcome remains unconfirmed, that entry cannot
be loaded, spawned, or saved; it stays listed so you can retry Delete.

Artwork stays in the data directory. Session membership and server access
settings also stay in local files. Run one app process per data directory.

## Status additions

Plugin status defaults come from the selected version's `statuses.json` and are
read-only. Each tabletop stores only its own additions in `settings`:

```json
{
  "_id": "unit_statuses/TABLETOP_ID",
  "schemaVersion": 2,
  "options": [
    { "value": "Marked", "color": "#123456" }
  ]
}
```

The initial additions list is empty. The picker, accepted values, and token
colors use the plugin's ordered defaults followed by these additions. Names
cannot duplicate defaults. The combined list contains 1–256 unique UTF-8 names,
at most 80 characters each without surrounding whitespace, and `#RRGGBB` colors.
Restart after editing additions in MongoDB; no status management UI is included.

## Initialization, backups, and limits

New tabletops store a plugin ID/version/fingerprint binding and initialize empty
owned map/unit libraries and status additions. No plugin default documents or
artwork are imported into MongoDB or session storage. Saving a default map creates
an owned map document with its embedded board and a plugin artwork reference;
further saves update that owned document. Defaults cannot be deleted.

The `library_initialization` collection distinguishes initialized empty owned
libraries from unfinished initialization. Failed session initialization remains
owner-only with Retry and Delete controls. There are no plugin import phases or
staged assets to resume. Old per-tabletop imports and data migration are outside
this change; create new bound tabletops.

Backups also require every exact referenced installed plugin folder, including
read-only operator mounts and older built-in versions retained across upgrades.
The existing file backup scripts cover application data, not external plugins.
See [plugin deployment](plugins.md) for the directory layout and retention rules.

Back up MongoDB, the data directory, and required plugin folders together while
the app is stopped.
Existing file backup/restore scripts cover only the data directory. For the
bundled database, make a separate compressed database archive:

```sh
./compose/compose.sh stop tabletop
./compose/backup.sh backups/tabletop-files.tar.gz
./compose/compose.sh exec -T mongo mongodump --archive --gzip > backups/tabletop-mongo.archive.gz
./compose/compose.sh start tabletop
```

Choose new backup filenames each time. Restore both archives from the same
backup into new, unused volumes. For the bundled service:

```sh
./compose/compose.sh stop tabletop mongo
export TABLETOP_DATA_VOLUME=tabletop-restored-data
export TABLETOP_MONGODB_DATA_VOLUME=tabletop-restored-mongodb-data
export TABLETOP_MONGODB_CONFIG_VOLUME=tabletop-restored-mongodb-config
./compose/restore.sh backups/tabletop-files.tar.gz
./compose/compose.sh up -d --wait mongo
./compose/compose.sh exec -T mongo mongorestore --archive --gzip --stopOnError < backups/tabletop-mongo.archive.gz
./compose/compose.sh up -d tabletop
```

Finish both restores successfully before starting the app. Keep all three volume
names in `compose/.env` afterward. Restoring only files leaves MongoDB at its
current state and can disconnect map/unit documents from their artwork. For an
external server, restore into a new database and update the app's URI and database
settings before starting it.

Use the same database name as the backup. Filesystem quotas exclude MongoDB
bytes; manage database capacity separately. Artwork is retained if a MongoDB
create fails because a timeout can leave the database write's outcome uncertain.

The app checks encoded BSON size before imports and saves and rejects documents
over 16 MiB. Existing board limits remain: 500 tokens, 1,000 strokes, 5,000 fog
marks, 64 rulers, and 50,000 points shared between pen and fog.

## Future optimization

- [ ] If board limits increase, separate strokes and ordered fog into documents
  or bounded batches referenced by the map's saved-board ID. Preserve fog order
  and finish writing the saved board before changing the map's reference. Keep
  units and rulers embedded unless their growth warrants separation.

## Verification

Integration tests use randomly named databases and remove them afterward:

```sh
TABLETOP_TEST_MONGODB_URI=mongodb://127.0.0.1:27017 go test -race ./...
```

Without this variable, database integration tests are skipped. BSON round-trip
and size checks still run.
