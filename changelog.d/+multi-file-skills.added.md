**Skills can now be a directory rather than a single file.** Set
`multi_file: true` on a ConfigMap-backed skill and the whole ConfigMap is
mounted as a directory, with every Markdown key copied into the skill's
directory. That allows a short `SKILL.md` that points at reference files the
agent loads only when it needs them, instead of one file that costs context
on every run. `key` is ignored for these; the skill directory must contain a
`SKILL.md`, and `setup-claude.sh` warns when it does not. Ported from the
First Responder fork, where the incident-classifier skill outgrew a single
file. Single-file skills are unchanged.
