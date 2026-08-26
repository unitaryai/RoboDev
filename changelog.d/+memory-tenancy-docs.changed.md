**Corrected what the memory docs claim about tenancy.**
`docs/concepts/memory.md` described per-customer isolation ("tenant A's
knowledge is never exposed to tenant B"), which was never the design: a
tenant is a use case, and there is no per-customer or per-repository
partition. It also documented `tenant_isolation` as a working switch. That
field is declared in the config struct and read by nothing; isolation is
unconditional and setting it to `false` does nothing. Both are now stated
plainly, along with what a tenant actually is and the fail-open behaviour of
an untenanted query.
