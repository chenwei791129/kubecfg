# kubecfg

Group the contexts in your kubeconfig and hide whole groups from `kubectl`.

Built for working alongside coding agents: disable your `prod` group before an
agent session and the production contexts are simply not in `~/.kube/config`,
so nothing running `kubectl` can reach them by accident.

> kubecfg guards against **accidental** use only. Disabled contexts are moved
> to a plaintext file that any process with file access can read.

## Install

```sh
go install github.com/chenwei791129/kubecfg@latest
```

## Usage

```sh
kubecfg                  # TUI: assign contexts to groups, toggle groups
kubecfg disable prod     # hide every context in the "prod" group
kubecfg enable prod      # bring them back
kubecfg status           # groups, ungrouped contexts, drift
kubecfg help
```

TUI keys: `↑/↓` move · `←/→` move the context to the previous/next group
(cycling through ungrouped and every group) · `g` pick a group from a list or
create a new one · `u` ungroup · `space`/`e` toggle the group · `q` quit.
Changes apply immediately; moving a context into a disabled group hides it
(and clears `current-context` if it was the current one, even if you only
pass through that group with `←/→`).

## How it works

- A context belongs to at most one group. Ungrouped contexts are always visible.
- Disabling a group moves its contexts, plus copies of their clusters and
  users, from the active kubeconfig into `~/.config/kubecfg/stash.yaml`.
  Clusters/users still used by other contexts stay in place.
- If `current-context` is disabled it is cleared, so `kubectl` fails instead
  of silently picking another cluster. It is not restored on enable.
- `enable`/`disable` are idempotent. If a tool such as
  `gcloud container clusters get-credentials` re-adds a context of a disabled
  group, `status` reports **drift**; re-run `disable` to fix it. On name
  conflicts the entry in the active kubeconfig wins.
- Group membership survives deleting a context, so a context re-added under
  the same name rejoins its group. Remove stale names by editing `groups.yaml`.
- Before each write the previous kubeconfig is saved as
  `~/.kube/config.kubecfg.bak`.
- The active kubeconfig is `$KUBECONFIG` (a single path) or `~/.kube/config`.

Group membership lives in `~/.config/kubecfg/groups.yaml`; edit it by hand to
rename or delete groups. Enable a group before deleting it, otherwise its
contexts stay in the stash with no group (fix with `u` in the TUI):

```yaml
groups:
  prod:
    enabled: false
    contexts: [prod-a, prod-b]
  stg:
    enabled: true
    contexts: [stg-a]
```

### Recovery without kubecfg

The stash is a regular kubeconfig:

```sh
KUBECONFIG=~/.kube/config:~/.config/kubecfg/stash.yaml kubectl config view --flatten > merged
```

## License

MIT
