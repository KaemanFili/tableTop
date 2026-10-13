# D&D 5e Monsters

Version 1 contains all 3,617 entries in the public, unfiltered
[D&D Beyond monster catalog](https://www.dndbeyond.com/monsters), retrieved on
October 13, 2026 across 181 pages. It includes current, Legacy, variant,
adventure, and partner entries from that listing.

## Using the plugin

Open the authenticated lobby and choose **D&D 5e Monsters v1** when creating a
session. The plugin is discovered from `plugins/dnd-5e/1/`; no import or server
restart is needed. Existing sessions retain their selected plugin. The local
`-dev` board continues to use the built-in plugin.

All entries are NPC tokens with the listed creature size. The 158 entries listed
as “Medium or Small” use Medium; both sizes occupy one grid square in tableTop.
Create a copy to choose Small or adjust any other token settings. This pack
provides the app's standard condition colors and eleven environment maps. It
contains token names, sizes, and artwork, rather than combat stat blocks.

## Default maps

Open **Load Existing Map** from the GM map controls to choose an environment.
Each default starts with an empty board. Use **Create copy**, or save a board
using that map, to keep your own encounter while leaving the default available.

| Map | Grid squares |
| --- | --- |
| Open Field | 30 × 30 |
| Desert Oasis | 30 × 30 |
| City Square | 30 × 30 |
| Mountain Pass | 30 × 30 |
| Village Center | 30 × 30 |
| Forest Clearing | 30 × 30 |
| River Crossing | 30 × 30 |
| Sailing Boat | 20 × 20 |
| Road with a Wagon | 20 × 20 |
| Ancient Ruins | 30 × 30 |
| Rocky Outcropping | 30 × 30 |

The maps are 1254 × 1254 PNG images in `1/maps/`, generated with the built-in
image_gen tool. They use a consistent top-down fantasy style with no baked-in
grid, labels, creatures, or tokens; the app supplies the grid and live encounter
state. All maps use five feet per grid square. Their definitions are in
[`1/maps.json`](1/maps.json); the full generation prompts are recorded in
[`1/map-artwork.json`](1/map-artwork.json).

## Names and artwork

Each key uses the catalog's numeric monster ID (`monster-5194867`, for example),
so repeated names remain separate entries. Legacy entries carry a `(Legacy)`
suffix. Other repeated labels include the source book; labels that would exceed
the app's 80-character limit or still repeat use the monster ID as a suffix.
`catalog.json` retains the original names, sizes, source books, monster links,
Legacy flags, and artwork URLs for every entry.

The pack includes 2,760 individual 100 × 100 listing thumbnails. These images are
stored locally so spawning tokens does not depend on D&D Beyond being available.
The other 857 catalog listings provide a creature-type icon rather than an
individual portrait; those entries use the app's default NPC artwork. Their keys
are recorded in `catalog.json` under `monstersWithoutArtwork`.

Artwork comes from the linked D&D Beyond listings. Original artwork ownership
remains with its respective creators and publishers; this pack does not grant a
separate artwork license.

## Retention and updates

Keep the entire version directory when deploying or backing up the app. Sessions
pin the plugin fingerprint, and owned copies retain references to these images.
Do not edit this version after sessions use it. Put catalog refreshes, higher
resolution images, or additional portraits into a new version directory instead.
