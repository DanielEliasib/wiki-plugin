# Wiki

A local, Obsidian-compatible, PARA-structured markdown vault plus the `wiki` CLI and Claude Code skill that let an agent capture, find, and tend notes in it.

## Language

**Vault**:
The folder of markdown notes (default `~/notes/wiki`, configurable), organised into Projects, Areas, and Resources.
_Avoid_: wiki (that names the tool and skill), notes folder

**Note**:
One markdown file in the vault, atomic (one fact, decision, event, or recipe) or a topic page.
_Avoid_: page, entry, document

**Session map**:
The block of at most about 750 tokens shown to the agent once, at session start. It lists every Project and a relevant subset of notes, so the agent knows what the vault holds.
_Avoid_: index, summary, context dump

**Nudge**:
What the session map is for: prompting the agent to search the vault before working. It points at where knowledge lives and never replaces searching.
_Avoid_: reminder, hint

**Pin**:
A Note marked `pinned: true` in its frontmatter. Pinned Notes always appear in the session map ahead of scored ones.
_Avoid_: favourite, bookmark

**Backlink count**:
How many `[[wikilinks]]` across the Vault point at a Note, by basename, ignoring links from generated vault indexes, from `_overview.md` files (authored lists would otherwise inflate the notes they enumerate), and from a Note to itself. One input to the relevance score that picks the session map's subset of Notes.
_Avoid_: popularity, usage

**Vault index**:
The `_index.md` file in each Project, Area, and Resource folder: a generated list of links to that folder's notes. Never hand-edited.
_Avoid_: index (alone), table of contents

**Overview**:
The optional, hand-maintained `_overview.md` in a Project, Area, or Resource folder: authored prose such as purpose, status, and decisions. Its frontmatter may carry `summary:` (the one-line gist shown in the session map) and `paths:` (directories that map a session's working directory to this folder). It carries no duty to list notes.
_Avoid_: hub page, readme, hand-maintained index

**Browse**:
Walking the vault top-down through the vault indexes (all Areas and Projects, then one folder's notes) instead of searching by query.
_Avoid_: navigate, explore

**Search index**:
The `.wiki/index.sqlite` file behind `wiki search`'s semantic matches. Rebuildable from the vault alone.
_Avoid_: index (alone), database, embeddings
