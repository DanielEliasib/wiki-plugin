# wiki

A local, Obsidian-compatible, PARA-structured markdown vault plus a single
Go CLI (`wiki`) for grep + local semantic search, reindexing, and
maintenance review. No database server, no daemon, no cloud dependency —
the vault is just files, and `wiki` is a thin tool for finding and tending
them.

## Configuration

Settings resolve in this order: `--vault` flag, environment variable, config
file, built-in default. The config file is `wiki/config.yaml` in your user
config directory (`~/.config/wiki/config.yaml` on Linux); set `WIKI_CONFIG`
to use another path.

```yaml
vault: ~/notes/wiki            # default; also WIKI_ROOT or --vault
embed_worker_dir: ~/src/wiki_skill/embed_worker   # also WIKI_EMBED_WORKER_DIR
```

`embed_worker_dir` must point at this repo's `embed_worker/` directory
(needs [uv](https://docs.astral.sh/uv/)). It is found automatically when the
binary is `bin/wiki` from `make build`; with `make install` you must set it.

## Vault Structure

```
projects/<slug>/   # deliverable-driven work, usually maps to a codebase
areas/<slug>/      # ongoing responsibility, no end state
resources/<slug>/  # reference material not tied to one project/area
archive/           # closed-out projects/areas, moved here wholesale
.wiki/
  index.sqlite     # semantic search index — do not hand-edit
  conventions.md   # shared filing rules, vault-side copy of this README's rules
```

Each `projects|areas|resources` folder has a generated `_index.md` (links
only, rewritten by `wiki reindex` and `wiki watch`, never hand-edited) and an
optional hand-written `_overview.md` with authored prose plus a `summary:`
(one-line gist) and `paths:` (directories that map a session's working
directory to the folder).

## Frontmatter (every note)

```yaml
---
title:
date: YYYY-MM-DD
type: atomic | topic
area: <slug>
tags: [tag1, tag2]
pinned: true              # optional: always listed in the session map
superseded_by: [[note]]   # optional
---
```

- **Atomic note** (default) — one fact/decision/event/recipe per file.
- **Topic page** — long-form, only when explicitly wanted.
- **Overview** (`_overview.md`) — the one hand-edited file per folder;
  everything else is written once and left alone.

The core rule: **never rewrite an old atomic note to keep it "current."**
When something changes, write a new atomic note and set `superseded_by:
[[new-note]]` on the old one. This is what removes
the "go update a bunch of notes" chore that motivated this whole design.

## Use Cases — Claude Code (the agent)

- **Capture new information.** Ask Claude to remember a fact, decision, or
  recipe — project-related or personal. It picks the right bucket
  (Project/Area/Resource), defaults to an atomic note, searches first to
  link related notes via `[[wikilink]]`.
- **Write a long-form page.** Explicitly ask for a deep-dive, tutorial, or
  project overview and Claude writes a `type: topic` page instead of an
  atomic note.
- **Recall something.** Ask Claude to find a past note — it runs `wiki
  search`, combining grep and semantic recall, so it can find things even
  when your phrasing doesn't match the note's exact words.
- **Record an update without editing history.** Tell Claude something
  changed and it writes a new atomic note with `superseded_by:` rather than
  silently rewriting the old one.
- **Review vault health.** Ask Claude to run `wiki prune` and interpret the
  report (likely-duplicate clusters, stale folders).
  Claude never merges/archives/deletes on its own — it presents the report
  and only acts on what you approve.
- **Process drafts.** Jot freeform notes into `drafts/` during the day,
  then ask Claude to process them — it files each one into a proper
  atomic/topic note per the rules above and deletes the draft once filed.

## Use Cases — You, Manually

- **Write a note by hand.** Pick a bucket folder, create a `.md` file with
  the frontmatter above. No tool needed — this is deliberately just files;
  `wiki reindex` refreshes the generated `_index.md`.
- **Browse/edit in Obsidian.** The vault is plain Obsidian-compatible
  markdown — open your vault folder as a vault for graph view,
  backlinks, and visual editing.
- **Read/edit in the terminal.** Day-to-day reading happens fine in nvim or
  any editor — nothing here depends on Obsidian being open.
- **Search from the CLI:**
  ```bash
  wiki search "some query" --top-k 10
  ```
  Flags can go before or after the query. Prints grep matches, then
  semantic matches from the local index.
- **Browse the vault:**
  ```bash
  wiki index          # every Project/Area/Resource with its gist
  wiki index <name>   # one folder's notes
  ```
- **See the session map** Claude gets at session start:
  ```bash
  wiki map --cwd "$PWD"
  ```
  At most ~750 tokens; `wiki map --hook` is what the SessionStart hook
  runs (registration snippet in `reference.md`).
- **Reindex after manual edits:**
  ```bash
  wiki reindex
  ```
  Full rebuild (also regenerates every `_index.md`) — run this after bulk
  edits, or if the Search index ever looks stale.
- **Keep the index live while editing:**
  ```bash
  wiki watch
  ```
  Runs in the foreground and incrementally re-embeds notes as they change
  on disk (create/edit/delete), so semantic search stays current without
  remembering to run `wiki reindex` — useful in a spare terminal/tmux pane
  while editing in Obsidian or nvim. Stop with Ctrl+C; not started
  automatically by anything.
- **Drop quick notes in `drafts/`:**
  A freeform capture folder at the vault root — no frontmatter, no naming
  convention. Jot things down through the day, then ask Claude to process
  drafts later; it files each one into a proper atomic/topic note and
  deletes the draft.
- **Review vault health from the CLI:**
  ```bash
  wiki prune
  ```
  Same report Claude would see — similar-note merge candidates, stale
  project/area folders. Report-only, never mutates.
- **Point at a different vault:**
  ```bash
  wiki search "..." --vault /path/to/other-vault
  ```
  Works identically for `reindex` and `prune`.

## Install

```bash
make build     # -> ./bin/wiki
make install   # -> $GOPATH/bin/wiki (on $PATH if that's already set up)
make test      # go test ./...
```

The Claude Code skill itself lives at `~/.claude/skills/wiki`; symlink this
repo there.

## Repo Layout

```
cmd/wiki/main.go       # CLI entrypoint, subcommand dispatch
internal/vault/        # frontmatter I/O, vault root resolution
internal/index/        # sqlite-backed semantic index
internal/embedworker/  # spawns the Python embedding worker
internal/wikicmd/      # search / reindex / prune command logic
embed_worker/          # thin fastembed wrapper (the only Python left)
vault-templates/       # conventions.md template copied into new vaults
SKILL.md               # what Claude Code reads to use this skill
```
