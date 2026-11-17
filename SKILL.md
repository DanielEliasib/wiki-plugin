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
- Search leaves out logs, sources files and the archive (below). Add `--logs` for the history (when something was done, where a fact came from), `--archive` for retired notes and closed-out folders.

## Capturing

1. Pick the bucket: Project (has a deliverable), Area (ongoing, no end state), or Resource (reference, not tied to one of those).
2. Default to an **atomic note**: one fact, decision, event, or recipe per file. Write a **topic page** only when asked for a long-form writeup or the content is inherently tutorial-shaped.
3. Search first (above). Link related notes with `[[filename-stem]]`, never with a relative path: notes move between subfolders and into `archive/`, and a stem link survives that.
4. Never rewrite an old atomic note to keep it current. Write a new note and set `superseded_by: [[new-note]]` on the old one.
5. File it by the folder layout below.

### Folder layout

Every Project, Area and Resource folder has the same shape; create a subfolder only when it gets its first file.

```
<bucket>/<folder>/
  _overview.md   what is true now: purpose, status, decisions in force, what is open
  _index.md      generated, never edited
  notes/         atomic and topic notes
    decisions/   one decision per file
    questions/   open questions only
    <kind>/      a kind with about five notes or more: specs/, plans/, meetings/, guides/, research/ ...
  drafts/        this folder's unfinished notes: freeform, no frontmatter
  logs/          dated logs: maintenance-log.md, build-log.md, <topic>-log.md ...
  raw/           source material, not notes: imports, exports, data, scripts, PDFs
  _sources.md    optional: where the facts came from
```

- **Kinds** under `notes/` are lowercase plurals, one level deep. A note goes straight into `notes/` until its kind reaches about five notes; then they move to `notes/<kind>/` together.
- **A superseded decision**, and **an answered question** (its answer now a decision or note), move to the archive under the same path: `archive/<bucket>/<folder>/notes/...`. So do notes that describe something gone.
- Nothing else sits at the folder's top level.

### Notes, logs and sources

Each folder keeps three kinds of file apart:

- **Notes** say what is true now. A topic page or `_overview.md` that changes is rewritten to the current state; an atomic note is superseded (step 4). Dated "Update (YYYY-MM-DD)" sections and session-by-session narration in a note are history: they go in a log.
- **Logs**, in `<folder>/logs/`, record what happened and when, as dated entries (`## YYYY-MM-DD`, newest last), each linking the notes it touched. One log per stream, named for it: `maintenance-log.md` for upkeep and changes, `build-log.md` for building a thing, `<topic>-log.md` for a running record such as meter readings, experiment runs or meetings. Add the entry to the log whose stream it belongs to; create a log when a stream first needs one. A note that is itself a dated record (readings, runs, a session diary) is a log: it lives in `logs/`.
- **Sources**, `<folder>/_sources.md`, records where facts came from (raw files, measurements, URLs), when that provenance is worth keeping.

Search and the session map leave logs, sources and the archive out; `wiki search --logs` and `--archive` include them.

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
