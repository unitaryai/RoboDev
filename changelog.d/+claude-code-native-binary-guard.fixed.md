**The agent image was one base-image bump away from shipping a broken Claude
Code.** The CLI installs a thin JS launcher and downloads its platform-native
binary from a postinstall script. npm 11 and later block install scripts by
default, so the postinstall is skipped, the native binary never arrives, and
`claude` fails at launch with "native binary not installed" — from an image
that built perfectly cleanly. `node:22-slim` still pins npm 10, where scripts
run, so this was latent rather than live; the First Responder fork hit it in
production on 2026-07-31 after upgrading npm. The install now passes
`--allow-scripts` for the package, verified working on both npm 10 and npm
12, and the build runs `claude --version` afterwards so a missing native
binary fails the build instead of reaching a registry.
