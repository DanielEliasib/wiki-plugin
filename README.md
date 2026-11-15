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
to use another path. It is read once per command, and a file that does not
parse is an error (naming the file), not ignored.

| Setting | Flag | Environment | Config key | Default |
|---|---|---|---|---|
| Vault | `--vault` | `WIKI_ROOT` | `vault` | `~/notes/wiki` |
| Embedding endpoint | | `WIKI_EMBED_URL` | `embed_url` | none: the local worker |
| Endpoint model name | | | `embed_model` | none |
| Worker directory | | `WIKI_EMBED_WORKER_DIR` | `embed_worker_dir` | `../embed_worker` next to the binary |
| Worker device | | `WIKI_EMBED_DEVICE` | `embed_device` | `cpu` |

```yaml
vault: ~/notes/wiki
embed_worker_dir: ~/src/wiki_skill/embed_worker
embed_device: cpu              # "gpu" for NVIDIA (CUDA 12)
embed_url: http://localhost:8080/v1/embeddings   # optional, see below
embed_model: bge-small         # optional: the "model" field sent to embed_url
```

With `embed_url` set (any OpenAI-compatible `/v1/embeddings` endpoint),
embeddings come from there and `embed_device` is ignored; if the endpoint
cannot be reached, the local worker stands in. The endpoint must serve the
same model as the worker, or the vectors will not match the index.
The index records its vector size: after changing the embedding model, search
and `wiki watch` refuse the old index with an error that says to run
`wiki reindex --full`.

`embed_worker_dir` must point at this repo's `embed_worker/` directory. It is
found automatically when the binary is `bin/wiki` from `make build`;
otherwise `wiki install` writes it for you.

`embed_device: gpu` installs several GB of CUDA wheels into the worker's
venv; the default `cpu` runs anywhere. Switching device re-syncs the venv.

## Vault Structure

```
projects/<slug>/   # deliverable-driven work, usually maps to a codebase
areas/<slug>/      # ongoing responsibility, no end state
resources/<slug>/  # reference material not tied to one project/area
archive/           # closed-out projects/areas, moved here wholesale
.wiki/
  index.sqlite     # semantic search index — do not hand-edit
  conventions.md   # shared filing rules, written by `wiki init`
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
  search`, combining keyword ranking (BM25; file names and titles weigh
  most) with section-level semantic recall, so it finds a note named after
  the thing asked about and also one that only matches in meaning.
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
  Flags can go before or after the query. Prints one entry per note, best
  first: path, how it matched (keyword, semantic or both), the semantic
  similarity, title and date, and the section that matched. Exact-phrase
  matches of the whole query follow (these need [ripgrep](https://github.com/BurntSushi/ripgrep)
  on the `PATH`; without `rg` they are left out). Without an index
  (`wiki reindex`) only keyword ranking runs. If the semantic half fails (the
  embedding service is down, or the index was built with another model) the
  search still answers by keywords and says why in a warning line (`warning`
  in `--json`). Hidden folders (`.claude/`, `.wiki/`, ...) are never indexed
  or searched.
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
  wiki reindex          # only changed notes are embedded again
  wiki reindex --full   # rebuild from scratch (after changing the embedding model)
  ```
  Also regenerates every `_index.md`. Incremental by default: notes whose
  text is unchanged keep their embeddings, and index entries whose file is
  gone (or sits in a hidden folder) are dropped. A note that cannot be read
  (broken frontmatter) is listed and skipped; the rest is still indexed.
- **Keep the index live while editing:**
  ```bash
  wiki watch
  ```
  Runs in the foreground and incrementally re-embeds notes as they change
  on disk (create/edit/delete), so semantic search stays current without
  remembering to run `wiki reindex` — useful in a spare terminal/tmux pane
  while editing in Obsidian or nvim. Stop with Ctrl+C; not started
  automatically by anything. A note that fails to update (unreadable, or the
  embedding service is down) is tried again after 5 s, 30 s and 2 min, then
  left until it changes again; `wiki reindex` catches up on anything missed.
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

Prerequisites: [Go](https://go.dev/dl/), [uv](https://docs.astral.sh/uv/)
(runs the embedding worker) and [ripgrep](https://github.com/BurntSushi/ripgrep)
(exact-phrase matches in `wiki search`). `wiki install` warns about any that
is missing.

```bash
make install-skill
```

That runs `go install` (put `$(go env GOPATH)/bin` on your `PATH`), then
`wiki install`, which shows what it will change and asks before doing it:

1. links this checkout into `~/.claude/skills/wiki` (or `$CLAUDE_CONFIG_DIR`),
2. writes the config file if you have none, with `embed_worker_dir` filled in,
3. adds the `wiki map --hook` SessionStart hook to `settings.json` (the
   original is kept as `settings.json.bak`).

Anything already in place is skipped, so it is safe to re-run. Use
`wiki install --dry-run` to preview, or `--yes` to skip the prompt.

Then create a vault and build the index:

```bash
wiki init      # creates the folders and .wiki/conventions.md (adds only what is missing)
wiki reindex   # first run downloads the embedding model (~130 MB) from Hugging Face
```

Restart Claude Code to load the skill and hook. Other targets: `make build`
(`./bin/wiki`), `make test`.

## Repo Layout

```
cmd/wiki/main.go       # CLI entrypoint, subcommand dispatch
internal/config/       # config file and the resolved settings (flag, env, file, default)
internal/vault/        # frontmatter I/O and the rule for what is a Note
internal/install/      # skill link, config and hook installer
internal/index/        # the Search index: sqlite file, atomic writes, vector size
internal/ranking/      # keyword + semantic fusion, best section and lines (no I/O)
internal/keyword/      # BM25 over notes and sections
internal/embedworker/  # embeddings: the endpoint, or the Python worker
internal/wikicmd/      # search / reindex / watch / prune / init command logic
embed_worker/          # thin fastembed wrapper (the only Python left)
SKILL.md               # what Claude Code reads to use this skill
```
