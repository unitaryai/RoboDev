**Task secret declarations are now bounded.** The `osmia:secrets` comment
block is parsed straight out of ticket and incident descriptions, which are
semi-trusted input, with no limit on how much of it reached the YAML decoder
or how many backend calls the result could trigger. A single block is now
capped at 64 KiB and a task may declare at most 32 secrets across its
description and labels combined, enforced in `Resolver.Resolve` so every
caller is covered. Recursive anchor expansion was already handled twice over,
by the decode target type and by `gopkg.in/yaml.v3`'s own aliasing guard;
`TestAliasBombIsRejected` now pins both so a future change cannot quietly
remove them.
