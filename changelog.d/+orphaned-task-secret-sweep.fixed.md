**An ephemeral task Secret stranded by a controller crash is now collected.**
A Secret that reaches adoption carries an ownerReference to its Job and is
garbage-collected with it, and a launch that fails in-process deletes its own
Secret. Neither covers the controller being killed between the two, which
left a Secret holding plaintext credentials that nothing would ever remove.
`sweepOrphanedTaskSecrets` runs on every reconciliation tick and deletes task
Secrets that have no ownerReferences and are more than 15 minutes old. It
selects on a new `osmia.io/secret-purpose` label rather than cross-referencing
live Jobs, so it cannot race with an adoption in progress: a Secret either has
an owner, in which case Kubernetes owns its lifecycle, or it never got one and
never will. It runs unconditionally, including when the controller is at its
concurrent job limit.
