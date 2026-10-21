# Wiki Conventions

This file is the single shared ruleset for this vault,
replacing the old per-topic `wiki.config.md` files. See the `wiki` skill's
`SKILL.md` (in the wiki skill repo) for the full workflow —
this file is the vault-side copy of the load-bearing rules.

- Structure: `projects/`, `areas/`, `resources/`, `archive/`, each folder
  containing a hand-maintained `_index.md` plus notes.
- Default note type is **atomic** (one fact/decision/event/recipe per
  file). Use a **topic page** only for explicit long-form writeups.
- Never edit an old atomic note to keep it current — write a new one and
  set `superseded_by:` on the old one instead.
- Frontmatter on every note: `title`, `date`, `type`, `area`, `tags`,
  optional `superseded_by`.
- Link notes with `[[wikilink]]` (filename stem, no `.md`) and keep each
  folder's `_index.md` pointing at current notes.
