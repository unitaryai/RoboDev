# Changelog fragments

One file per user-visible change. At release time `towncrier` collects them
into a new section of `CHANGELOG.md` and deletes them.

This exists so that no two pull requests ever edit the same lines. Appending
to a shared "Unreleased" section conflicts every time two changes are in
flight at once, which was costing this repository a rebase per merge.

## Adding one

Create a file named `+<slug>.<category>.md`:

```text
changelog.d/+wire-task-scoped-secrets.added.md
```

- `<slug>` is a short kebab-case description, usually your branch name minus
  its prefix. It only has to be unique among the open fragments.
- `<category>` is one of `added`, `changed`, `deprecated`, `removed`,
  `fixed`, `security`.
- The leading `+` marks a fragment with no associated issue number, which is
  the normal case here. A fragment named `123.fixed.md` is also valid and
  refers to issue or pull request 123.

The file contents are the changelog entry itself, as it should read in the
released notes. Write it as prose, without a leading `-`: towncrier adds the
bullet. British English, and the same standard as a commit message. Say what
changed and why it mattered, not just which files moved.

Purely internal changes that no user or operator would notice need no
fragment. A refactor that changes observable behaviour does.

## Building the changelog

Only done at release time, by a maintainer. Contributors never run this.

```bash
uvx towncrier build --version v0.5.0
```

To preview without consuming the fragments:

```bash
uvx towncrier build --draft --version v0.5.0
```

Configuration lives in `towncrier.toml` at the repository root. The full
release procedure is in `docs/contributing.md`.
