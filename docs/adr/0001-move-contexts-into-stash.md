# Disable contexts by moving them into a Stash file

kubecfg's goal is to keep agents from accidentally reaching production, so a
Disabled Group's contexts must be absent from the file `kubectl` reads, not
merely marked. We disable a Group by physically moving its context, cluster and
user entries out of the Active kubeconfig into a separate, self-contained
kubeconfig file (the Stash) and move them back on enable. This keeps
`~/.kube/config` as the single source of truth that `gcloud`, `kubectx`,
Rancher CLI and friends already write to, and needs no shell integration.

## Considered Options

- **`KUBECONFIG` multi-file**: split contexts across files and control
  visibility via the env var. Rejected: a TUI cannot change its parent shell's
  environment, every shell would need a hook, and the user does not use
  `KUBECONFIG` today.
- **Master file + generated view**: keep a full master and render
  `~/.kube/config` from it. Rejected: anything other tools write into
  `~/.kube/config` is lost on the next render unless we add two-way sync.

## Consequences

- The Stash is plaintext and readable by any process. This is by design: the
  threat model is accidental use, not a hostile agent.
- Recovery without kubecfg is always possible:
  `KUBECONFIG=~/.kube/config:~/.config/kubecfg/stash.yaml kubectl ...`.
- Entries added to the Active kubeconfig while their Group is Disabled are
  visible until `disable` is re-run; `status` reports this as Drift.
