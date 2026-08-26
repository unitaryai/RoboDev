**The GitLab CLI in the agent image is bumped and its download is now
verified.** `glab` was pinned at 1.79.0, over thirty minor releases behind,
and the tarball was fetched over `curl` with no integrity check at all before
being installed and run with the agent's GitLab token. It is now 1.115.0,
verified against the `checksums.txt` published with the release, with the
match anchored so a filename that merely contains the expected one cannot
satisfy it. The build fails if the checksum line is missing, if it does not
match, or if the resulting binary will not report its version. Renovate
metadata is attached to the pin so the version does not drift this far again.
