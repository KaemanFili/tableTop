# MongoDB storage

Set `TABLETOP_MONGODB_URI` to enable MongoDB for map and unit libraries and
status definitions. `TABLETOP_MONGODB_DATABASE` defaults to `tabletop`. Without
a URI, libraries use JSON files and statuses use their built-in definitions.

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

## Status document

Status names and colors are an ordered list in one server-wide `settings` document:

```json
{
  "_id": "unit_statuses",
  "schemaVersion": 1,
  "options": [
    { "value": "Poisoned", "color": "#65A30D" },
    { "value": "Marked", "color": "#123456" }
  ]
}
```

On first use, the app creates this document with the existing statuses. Later
starts read it without overwriting it. The list drives the picker, accepted status
names, and token colors. Restart after editing definitions in MongoDB; this
change does not add a status-editing UI.

Names must be unique, nonempty, valid UTF-8, and at most 80 characters, without
leading or trailing whitespace. Colors must use `#RRGGBB`. Between 1 and 256 definitions
are accepted. Invalid definitions stop startup. Removing a definition preserves
its name on saved tokens; its color becomes transparent until the definition is
restored or the token's status changes.

## Imports, backups, and limits

Each library imports its JSON file when first opened with MongoDB enabled.
Authenticated libraries open when their tabletop is first accessed. Legacy unit
entries retain their defaults and starter-unit deletions. Without a unit file,
the initial library contains Knight and Goblin.

The `library_initialization` collection tracks completed imports independently
of their documents, preserving intentionally empty libraries. Interrupted imports
can be retried before a board becomes available. Original JSON files remain, but
subsequent writes go only to MongoDB; disabling MongoDB would load those older files.

Back up MongoDB and the entire data directory together while the app is stopped.
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
