---
name: wiki
description: Search the personal markdown vault (the `wiki` CLI) for existing knowledge before researching, deciding, or configuring anything. Also use to save a note, decision, or recipe, including when one has just been settled; to find or recall something from past notes; or to tidy up the vault.
---

# Wiki

A local, Obsidian-compatible, PARA-structured markdown vault, managed with the
`wiki` CLI (Go binary on `$PATH`; install with `go install ./cmd/wiki` from
this repo). The session map shown at session start lists the vault's
folders; this skill is the procedure. The vault is found from `--vault
<path>`, `WIKI_ROOT`, or `vault:` in the config file, else `~/notes/wiki`
(see the README's Configuration section). Less common features are in
[reference.md](reference.md).

## Load the map

The session map (a "wiki vault" inventory) normally arrives at session start, and again after `/clear` and compaction. If this session has none (it was resumed, or started before the hook was installed), or the user invokes this skill with `map`, run `wiki map --cwd "$PWD"` once and read its output: it lists every folder with its gist and the notes most relevant to this directory.

## Search first

Before researching, deciding, configuring, or asking the user for a fact:

```bash
wiki search "<query>" --top-k 10
```

- Run one or two queries built from the task's nouns. Prints the best notes, one entry per note (ranked by keywords and by meaning, per section): path, title, date and the passage that matched; then any exact-phrase matches.
- Read the top hits, not just the titles. Use what you find and cite it by note title. Flag a hit that contradicts what you were about to do.
- Browse instead of searching when you want the inventory: `wiki index` lists every folder with its gist; `wiki index <name>` lists one folder's notes (`<kind>/<name>` if the name exists in two buckets).
- Skip the search for trivial edits and anything fully inside the current repo.
- On zero hits, say nothing, unless the task was a knowledge question: then say the vault had nothing.

## Capturing

1. Pick the bucket: Project (has a deliverable), Area (ongoing, no end state), or Resource (reference, not tied to one of those).
2. Default to an **atomic note**: one fact, decision, event, or recipe per file. Write a **topic page** only when asked for a long-form writeup or the content is inherently tutorial-shaped.
3. Search first (above). Link related notes with `[[filename-stem]]`.
4. Never rewrite an old atomic note to keep it current. Write a new note and set `superseded_by: [[new-note]]` on the old one.

Frontmatter on every note:

```yaml
---
title:
date: YYYY-MM-DD
type: atomic | topic
area: <slug>
tags: [tag1, tag2]
pinned: true            # optional: always listed in the session map
superseded_by: [[note]] # optional
---
```

Do not edit `_index.md`: it is generated (`wiki reindex`, or `wiki watch`) and lists a folder's notes. A folder may also have an optional hand-written `_overview.md` for purpose, status and decisions, with frontmatter `summary:` (one line, shown in the session map) and `paths:` (a list of directories; a session started inside one gets that folder's notes ranked first).

## Pruning (on demand only)

```bash
wiki prune
```

Reports similar-note merge candidates and stale Project/Area folders. It never acts on its own: show the report to the user and merge, archive or delete only what they approve.
