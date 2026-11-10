# AGENTS.md

Handoff notes for agents working on kubecfg. Read `README.md` for usage,
`CONTEXT.md` for the domain vocabulary (use its terms: Context, Group,
Ungrouped, Enabled/Disabled, Active kubeconfig, Stash, Drift), and
`docs/adr/` for decisions that must not be silently reversed.

## Purpose and threat model

kubecfg keeps coding agents from accidentally reaching production clusters by
moving whole Groups of contexts out of the Active kubeconfig. It defends
against **accidental** use only; the Stash is plaintext by design. Do not add
encryption, keychains or access control without an explicit request.

## Layout

Flat `package main`, intentionally (no `cmd/` or `internal/`):

| File | Role |
|---|---|
| `main.go` | CLI dispatch: no args = TUI, `enable`, `disable`, `status`, `help` |
| `kubeconfig.go` | Paths, load/save, move logic (`place`, `stash`, `restore`), `Rows`, `Status` |
| `groups.go` | `groups.yaml` load/validate/save, membership helpers |
| `tui.go` | bubbletea single-list TUI |
| `*_test.go` | Fixture-based tests with synthetic names |

State files: Active kubeconfig (`$KUBECONFIG` single path or `~/.kube/config`),
`~/.config/kubecfg/stash.yaml`, `~/.config/kubecfg/groups.yaml`.

## Invariants (tests cover these; keep them green)

- A context belongs to at most one Group; duplicates are a load error.
- Ungrouped contexts belong in the Active kubeconfig.
- `enable`/`disable` are idempotent reconciles; on same-name conflicts the
  Active kubeconfig wins.
- A cluster/user leaves the Active kubeconfig only when no remaining context
  references it; unreferenced entries already there are never touched.
- Disabling the current context clears `current-context`; enabling never
  restores it.
- Write order: the destination file first, stop on the first error, then
  `groups.yaml`. A failed or interrupted run may duplicate, never lose.
  Writes are temp file + fsync + rename + dir fsync, following symlinks.
- One rolling backup `<active>.kubecfg.bak` before every Active write.
- Membership of deleted contexts is **kept** (no pruning) so a re-added context
  rejoins its Group.
- The TUI reloads state from disk before every change.
- Rows are sorted by context name only, so rows do not jump on group changes.

## Deliberate non-features

Switching `current-context` (use kubectx), renaming/deleting groups (edit
`groups.yaml`), CLI group assignment, `--all`, `status --short`, file locking,
regex auto-grouping, multi-path `KUBECONFIG`.

## Known limitations (accepted)

- Disable can overwrite a Stash-only cluster/user when the Active kubeconfig has
  a different entry with the same name.
- Relative paths (e.g. `certificate-authority: certs/ca.crt`) are copied as-is,
  so the Stash is not usable on its own for those entries.
- Deleting a disabled group by hand leaves its contexts hidden in the Stash.
- `run()` exit codes and writes through symlinks have no tests.

## Working rules

- Never use real cluster, context, host or user names in code, tests, docs or
  commits; fixtures use `prod-a`, `stg-a`, `lab`, `*.example`.
- Never run kubecfg against the real `~/.kube/config` for testing; use a temp
  `HOME` with a copied kubeconfig and `unset KUBECONFIG`.
- Validate with `gofmt -l . && go vet ./... && go test ./...`.
- Commits follow Conventional Commits; releases are cut by release-please from
  `main` (tags `vX.Y.Z`). A Homebrew formula in `chenwei791129/homebrew-apps` is
  planned but not done yet.
