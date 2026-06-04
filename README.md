# gst

`gst` is a read-only Git status visualizer for people who want to understand the
shape of a repository before they run Git commands.

![Image](https://github.com/user-attachments/assets/09fd03cf-2781-4d9f-b73d-f057e9e1ac90)

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
go build -o tmp/gst ./cmd/gst
```

Release steps are documented in [docs/release.md](docs/release.md).

## Usage

```sh
gst
gst --interval 1s
gst --once
gst --no-color
```

By default, `gst` opens the interactive TUI. Use `tab` or the left/right arrow
keys to move between views, `1`-`8` to jump directly, `?` for help, `t` to
toggle native `git status`, `a` on the graph view to toggle the detailed
`--all` graph, `s` on the diff view to toggle worktree/staged diff, and copy
diffs from the diff view with `y` for worktree, `i` for staged/index, or `a`
for a full text patch from `HEAD` to the worktree, including untracked files;
binary file contents are omitted. Less-like keys scroll graph, diff, and long
list views: `j`/`k` for one line, `d`/`u` for half a
page, and `f`/`b` for one page. Use `r` to refresh, and `q` to quit. The
current tab's available keys are always shown in the footer. The
available views are:

- `overview`
- `graph`
- `files`
- `diff`
- `branches`
- `stash`
- `refs`
- `remote`

## What It Shows

- `sync`: the relationship between the current local branch and its upstream
- `workspace`: staged, modified, untracked, and conflicted file counts
  rendered as compact dashboard meters
- `commit graph`: recent commits across local and remote refs; graph view can
  toggle a detailed `--all` graph with `a` and scroll through older commits
- `changed files`: a compact list of working tree and index changes with
  explicit kind labels
- `diff`: the current worktree or staged patch, toggled with `s`
- `branches`: current branch, upstream, and local/remote branch relationships
- `stash`: temporary saved work outside the current branch
- `refs`: local/remote branches and tags with recent commit or tag ages
- `repository notes`: remotes, stashes, and non-fatal collection warnings
- `attention`: in-progress merge/rebase/cherry-pick/revert guidance when relevant

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
