**Edge images are rebuilt weekly so upstream security patches actually
ship.** The images were built only on a push to `main`, and nothing pins a
base-image digest, so a fix landing in `node:22-slim` or the Go builder
reached the published images only when someone happened to commit. A quiet
week meant the agent image kept running whatever the base looked like when it
was last touched. A Monday 05:17 UTC schedule now rebuilds them, deliberately off the hour because GitHub delays scheduled runs during the high-load window at the top of every hour, and every
build passes `pull: true` so the runner fetches the base afresh rather than
republishing the same bits from cache.

GitHub disables scheduled workflows in a public repository after 60 days
without repository activity, so the rebuild stops in exactly the quiet stretch
it exists for. That cannot be opted out of; `docs/contributing.md` records how
to spot it and re-enable.
