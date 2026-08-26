**Edge images are rebuilt weekly so upstream security patches actually
ship.** The images were built only on a push to `main`, and nothing pins a
base-image digest, so a fix landing in `node:22-slim` or the Go builder
reached the published images only when someone happened to commit. A quiet
week meant the agent image kept running whatever the base looked like when it
was last touched. A Monday 05:00 UTC schedule now rebuilds them, and every
build passes `pull: true` so the runner fetches the base afresh rather than
republishing the same bits from cache.
