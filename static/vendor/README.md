# HTMX

Vendored HTMX 2.0.11 from https://cdn.jsdelivr.net/npm/htmx.org@2.0.11/dist/htmx.min.js.
License: Zero-Clause BSD, included in `htmx-LICENSE`.

SHA-384 integrity value from https://htmx.org/docs/:
`sha384-2OatzQy1H+Zd/IIrjr1TcuDGqLXeHhbooAyJY1KdQMKnr4LZ22k31GBLdYKHmVjg`

To update, download an explicit version and its license, verify its checksum,
update the script URL in `index.html`, and run the browser regression tests.


# Dice Box Three.js

Vendored `@3d-dice/dice-box-threejs` 0.0.12 from
https://registry.npmjs.org/@3d-dice/dice-box-threejs/-/dice-box-threejs-0.0.12.tgz.
MIT license included alongside the standalone ES module.
Verified package integrity:

`sha512-Bau5UizUunZS9+A6zgGeZecEJotSLEI6YvmYTosvvdhRxv92xo89aIvKYQb6xqdx/AqjHdGENJmqBOadlFL9kA==`

This replaces Dice Box for animation because it supports fixed outcomes using
`2d6@3,4` notation. Textures and sounds are disabled, so no extra assets are needed.


The app adapter clears the library's stale d4 result after changing its faces.
Percentile outcomes are animated with a tens die and a units die; zero faces
use the engine's values 100 and 10. Mixed groups of the same type are combined
for animation while their modifiers and subtotals remain separate in the UI.
