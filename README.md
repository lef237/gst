# gst

`gst` is a read-only Git status visualizer for people who want to understand the
shape of a repository before they run Git commands.

Git beginners often struggle because the current state is split across several
places:

- commits and branches form a graph
- local branches and remote tracking branches can point at different commits
- the index and working tree can each contain different file changes

`gst` puts those pieces into one terminal dashboard. It does not push, pull,
checkout, commit, merge, rebase, or mutate the repository.

## Install

```sh
go install github.com/lef237/gst/cmd/gst@latest
```

For local development:

```sh
go run ./cmd/gst
```

To build a local binary:

```sh
go build -o ~/tmp/gst ./cmd/gst
```

## Usage

```sh
gst
gst --interval 1s
gst --once
gst --no-color
```

By default, `gst` opens the interactive TUI. Use `tab` to move between views,
`1`-`7` to jump directly, `?` for help, `r` to refresh, and `q` to quit. The
available views are:

- `overview`
- `graph`
- `files`
- `branches`
- `stash`
- `refs`
- `remote`

## What It Shows

- `sync`: the relationship between the current local branch and its upstream
- `workspace`: staged, modified, untracked, and conflicted file counts
- `commit graph`: recent commits across local and remote refs
- `changed files`: a compact list of working tree and index changes
- `branches`: current branch, upstream, and local/remote branch relationships
- `stash`: temporary saved work outside the current branch
- `refs`: local and remote branches with recent commit ages
- `repository notes`: remotes, stashes, and non-fatal collection warnings

The intended mental model is:

- `local ahead`: your machine has commits the remote cannot see yet
- `local behind`: the remote has commits your machine has not copied yet
- `diverged`: both sides have unique commits
- `index`: the next commit you are building
- `worktree`: the files currently on disk

## Design Constraints

- read-only Git commands only
- no network synchronization
- no dependency on a GitHub API token
- works as a plain CLI snapshot and as a lightweight watch-mode TUI
