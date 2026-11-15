# Wiki reference

Less common features of the `wiki` CLI. The everyday procedure is in [SKILL.md](SKILL.md).

## Vault layout

```
projects/<slug>/   # deliverable-driven work, usually maps to a codebase
areas/<slug>/      # ongoing responsibility, no end state
resources/<slug>/  # reference material not tied to one project or area
archive/           # closed-out projects and areas
.wiki/index.sqlite # semantic search index: rebuildable, never hand-edit
```

Each `projects|areas|resources` folder holds a generated `_index.md` (links only) and an optional hand-written `_overview.md`.

## Reindexing

```bash
wiki reindex          # incremental: embeds only changed notes, drops deleted ones
wiki reindex --full   # rebuild from scratch, e.g. after changing the embedding model
```

Updates the semantic search index, after first regenerating every `_index.md`. It goes through the same step as `wiki watch`, so it also drops index entries whose file is gone or lies in a hidden folder, and it lists notes it could not read (`failed` in `--json`) instead of stopping. A note is embedded again only when its text changed since it was indexed (the index stores a hash per note), so a run over an unchanged vault embeds nothing. `--full` re-embeds every note and keeps the old index if that fails. **This overwrites every `_index.md`**, so a vault whose hand-written indexes have not yet been renamed to `_overview.md` loses that prose on the first run. Run it after bulk edits or if search looks stale. The notes to embed go in one batched call to the embedding worker.

## Watching

```bash
wiki watch
```

Runs in the foreground and incrementally re-embeds notes as they change; it also regenerates the affected folder's `_index.md` on add, remove or rename. Hidden folders are neither watched nor indexed. When a batch fails (a note cannot be read, or the embedding service is down) the other notes are still applied and the failed ones are tried again after 5 s, 30 s and 2 min; after that a path is left until it changes again, with a log line suggesting `wiki reindex`. Stop with Ctrl+C. Nothing starts it automatically.

## Drafts

`drafts/` at the vault root is a freeform scratch folder: no frontmatter, any subfolders. It is searchable like any note once indexed. When asked to process drafts: read everything under `drafts/`, file each into a proper atomic or topic note (pick the bucket, write frontmatter), then delete the draft.

## The session map

```bash
wiki map --cwd "$PWD"   # plain text, for inspecting by hand
wiki map --hook         # SessionStart hook: reads hook JSON on stdin, prints hook JSON
```

At most ~750 tokens: a nudge line, every folder with gist and note count (the folder whose `paths:` contains the working directory is tagged `← cwd`), then pinned notes, then the highest-scoring notes. Score = 1.0·ln(1+backlinks) + 1.0·0.5^(age/30 days) + 4.0 if the note is in a cwd-matched folder. It reads the vault live, never writes or embeds, and with `--hook` prints nothing and exits 0 on any error.

`wiki install` registers this hook for you. To do it by hand, add it to `~/.claude/settings.json`. The matcher keeps `resume` and `fork` silent (they already carry the map in their transcript):

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|clear|compact",
        "hooks": [{ "type": "command", "command": "wiki map --hook" }]
      }
    ]
  }
}
```

A new `source` value added by Claude Code will not match until the matcher is updated.

## Other vaults

`--vault <path>` (or `WIKI_ROOT`) works with every subcommand.

## Structured output

Every subcommand accepts `--json`. `search`, `reindex`, `prune` and `index` print one JSON object; `watch --json` streams newline-delimited events (`{"event":"watching",...}`, then `{"event":"batch","upserted":[...],"removed":[...]}`). With `--json`, errors print `{"error": "..."}` to stdout, so a single stdout parse handles success and failure. `map` and `install` have no `--json` and refuse the flag; `map --hook` emits hook JSON. `search` adds `warning` when it fell back to keywords only; `reindex` adds `failed` when notes could not be read.
