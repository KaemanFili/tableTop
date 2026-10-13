# Possible improvement: per-session containers and a shared plugin cache

This is a proposed architecture, not implemented behavior. Today, one Go process
hosts the lobby and all active tabletops, with an in-process plugin metadata cache.

## Session workers

Creating a session could start a dedicated Docker container running a Go process
for that tabletop. A central service would continue to handle authentication,
the lobby, membership, and worker lifecycle. It would route HTTP requests and
WebSocket connections under `/sessions/{id}/` to the session's worker.

Each worker would own that session's board state and broadcasts, use its existing
storage scope for owned content, and access installed plugin versions through a
read-only mount or the application image. A worker must only accept requests
authorized for its assigned session; routing alone does not establish membership
or GM permissions.

Keep one authoritative worker per session so players share the same board and
WebSocket broadcasts. Record worker readiness and routing before redirecting
players to a new session. Idle workers could stop and restart when someone opens
their session. A small pool of ready containers could reduce cold-start latency.

## Redis plugin cache

Move the reusable plugin metadata cache to Redis so session processes can share
validated content rather than independently reading and validating every pack.
Cache serialized unit definitions, map metadata, statuses, artwork references,
and the content fingerprint. Image files would remain in plugin storage;
decoded image pixels would not be part of this metadata cache.

Use keys containing the cache schema version, plugin ID, plugin version, and
content fingerprint. Include validation limits in the cache identity, or check
them before accepting an entry. Only publish a complete entry after validation
succeeds. Coordinate concurrent cache misses so several new workers do not all
rebuild the same pack. Expiration and a Redis memory limit would bound storage.

Redis would be a rebuildable cache, while installed plugin files remain the
source of truth. Cache hits must still enforce the session's pinned fingerprint
and detect unavailable or changed installations. A Redis outage should allow
workers to load and validate their installed content directly.

Workers would still deserialize metadata and may retain a small local working
set. Redis therefore reduces repeated validation work and centralizes cached
data; it does not eliminate each Go process's memory use.

## Tradeoffs and prerequisites

- Per-session containers provide separate process lifecycles and resource limits,
  but add Go runtime overhead, container startup time, routing, and operational
  complexity. This is primarily an isolation and scaling option; measure whether
  it improves latency or total memory before adopting it.
- Container creation belongs in a trusted orchestration component. Session
  workers should not need access to the Docker daemon.
- Persist and restore live board state before stopping idle workers. Currently,
  unsaved placed tokens and board utilities are held in memory and would be lost
  when their Go process stops.
- Define resource limits, startup timeouts, crash recovery, and session deletion
  cleanup, including closing player connections and removing the worker.
- Measure cold and warm session load times, memory per worker, Redis memory,
  cache-hit rates, and behavior during worker or Redis failures before rollout.

Start with a prototype using two sessions sharing one plugin version, then
compare it with the current single-process deployment.
