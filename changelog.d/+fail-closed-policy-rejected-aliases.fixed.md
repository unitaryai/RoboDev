**The fail-closed secret policy rejected aliases as well as raw
references.** `Resolver.Resolve` expanded aliases before validating, so by
the time the raw-reference rule was applied an `alias://` request had
already become the concrete URI that `allow_raw_refs: false` forbids.
Under the recommended configuration this rejected every secret, including
the aliases that setting exists to permit. The check is now split:
`ValidateRawRef` runs against what the task actually wrote, and
`ValidateResolved` (scheme and env-name patterns) runs against the
expanded URI.
