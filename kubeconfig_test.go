package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fixture = `apiVersion: v1
kind: Config
current-context: prod-a
clusters:
- name: prod-a
  cluster: {server: https://prod-a.example}
- name: prod-b
  cluster: {server: https://prod-b.example}
- name: stg-a
  cluster: {server: https://stg-a.example}
- name: orphan
  cluster: {server: https://orphan.example}
users:
- name: prod-a
  user: {token: a}
- name: shared
  user: {token: s}
- name: stg-a
  user: {token: c}
contexts:
- name: prod-a
  context: {cluster: prod-a, user: prod-a}
- name: prod-b
  context: {cluster: prod-b, user: shared}
- name: stg-a
  context: {cluster: stg-a, user: stg-a}
- name: lab
  context: {cluster: stg-a, user: shared}
`

const fixtureGroups = `groups:
  prod: {enabled: true, contexts: [prod-a, prod-b, gone]}
  stg: {enabled: true, contexts: [stg-a]}
`

func setup(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	p := Paths{Active: filepath.Join(dir, "config"), Stash: filepath.Join(dir, "kubecfg", "stash.yaml"), Groups: filepath.Join(dir, "kubecfg", "groups.yaml")}
	must(t, os.WriteFile(p.Active, []byte(fixture), 0o600))
	must(t, os.MkdirAll(filepath.Dir(p.Groups), 0o700))
	must(t, os.WriteFile(p.Groups, []byte(fixtureGroups), 0o600))
	return p
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func reload(t *testing.T, p Paths) *State {
	t.Helper()
	st, err := load(p)
	must(t, err)
	return st
}

func keys[V any](m map[string]V) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	slices.Sort(k)
	return k
}

func eq(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestDisableEnableRoundtrip(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).SetEnabled("prod", false))

	st := reload(t, p)
	eq(t, "active contexts", keys(st.Active.Contexts), []string{"lab", "stg-a"})
	eq(t, "active clusters", keys(st.Active.Clusters), []string{"orphan", "stg-a"}) // orphan untouched
	eq(t, "active users", keys(st.Active.AuthInfos), []string{"shared", "stg-a"})   // shared still used by lab
	eq(t, "stash contexts", keys(st.Stash.Contexts), []string{"prod-a", "prod-b"})
	eq(t, "stash users", keys(st.Stash.AuthInfos), []string{"prod-a", "shared"}) // stash is self-contained
	if st.Active.CurrentContext != "" {
		t.Errorf("current-context = %q, want cleared", st.Active.CurrentContext)
	}
	if st.Groups.Groups["prod"].Enabled {
		t.Error("prod still enabled")
	}
	// "gone" exists nowhere but keeps its membership; status counts only real contexts.
	eq(t, "prod contexts", st.Groups.Groups["prod"].Contexts, []string{"prod-a", "prod-b", "gone"})
	if out := st.Status(); !strings.Contains(out, "prod                 disabled  2") {
		t.Errorf("status count:\n%s", out)
	}
	if _, err := os.Stat(p.Active + ".kubecfg.bak"); err != nil {
		t.Errorf("no backup: %v", err)
	}

	must(t, st.SetEnabled("prod", true))
	st = reload(t, p)
	eq(t, "active contexts", keys(st.Active.Contexts), []string{"lab", "prod-a", "prod-b", "stg-a"})
	eq(t, "active users", keys(st.Active.AuthInfos), []string{"prod-a", "shared", "stg-a"})
	eq(t, "stash contexts", keys(st.Stash.Contexts), nil)
	eq(t, "stash users", keys(st.Stash.AuthInfos), nil)
	if st.Active.CurrentContext != "" {
		t.Errorf("current-context restored to %q, want empty", st.Active.CurrentContext)
	}
}

func TestDisableIsIdempotentAndFixesDrift(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).SetEnabled("prod", false))

	// Simulate `gcloud get-credentials` re-adding prod-a with a fresh token.
	st := reload(t, p)
	st.Active.Contexts["prod-a"] = st.Stash.Contexts["prod-a"]
	fresh := *st.Stash.AuthInfos["prod-a"]
	fresh.Token = "fresh"
	st.Active.AuthInfos["prod-a"] = &fresh
	st.Active.Clusters["prod-a"] = st.Stash.Clusters["prod-a"]
	must(t, writeKubeconfig(p.Active, st.Active))

	st = reload(t, p)
	if !strings.Contains(st.Status(), "prod-a (prod) is visible") {
		t.Errorf("status missing drift:\n%s", st.Status())
	}
	must(t, st.SetEnabled("prod", false))
	st = reload(t, p)
	eq(t, "active contexts", keys(st.Active.Contexts), []string{"lab", "stg-a"})
	if got := st.Stash.AuthInfos["prod-a"].Token; got != "fresh" {
		t.Errorf("stash token = %q, want active to win", got)
	}
	if strings.Contains(st.Status(), "Drift") {
		t.Errorf("drift remains:\n%s", st.Status())
	}
}

func TestEnableActiveWinsOnConflict(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).SetEnabled("prod", false))
	st := reload(t, p)
	st.Active.Contexts["prod-a"] = st.Stash.Contexts["prod-a"]
	newer := *st.Stash.AuthInfos["prod-a"]
	newer.Token = "newer"
	st.Active.AuthInfos["prod-a"] = &newer
	must(t, writeKubeconfig(p.Active, st.Active))

	must(t, reload(t, p).SetEnabled("prod", true))
	st = reload(t, p)
	if got := st.Active.AuthInfos["prod-a"].Token; got != "newer" {
		t.Errorf("token = %q, want active to win", got)
	}
	eq(t, "stash contexts", keys(st.Stash.Contexts), nil)
}

func TestAssignMovesContextToMatchGroup(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).SetEnabled("prod", false))

	must(t, reload(t, p).Assign("lab", "prod")) // into disabled group -> hidden
	st := reload(t, p)
	if st.Active.Contexts["lab"] != nil || st.Stash.Contexts["lab"] == nil {
		t.Error("lab should move to stash")
	}
	must(t, st.Assign("lab", "")) // ungrouped -> always visible
	st = reload(t, p)
	if st.Active.Contexts["lab"] == nil || st.Stash.Contexts["lab"] != nil {
		t.Error("lab should be back in active")
	}
	eq(t, "prod contexts", st.Groups.Groups["prod"].Contexts, []string{"gone", "prod-a", "prod-b"}) // missing name kept
	must(t, st.Assign("lab", "new"))                                                                // new group is created enabled
	st = reload(t, p)
	if g := st.Groups.Groups["new"]; g == nil || !g.Enabled || st.Active.Contexts["lab"] == nil {
		t.Errorf("new group = %+v", g)
	}
}

func TestFailedStashWriteLeavesActiveIntact(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	p := setup(t)
	ro := filepath.Join(filepath.Dir(p.Active), "ro")
	must(t, os.Mkdir(ro, 0o500)) // read-only: stash write fails
	p.Stash = filepath.Join(ro, "stash.yaml")
	if err := reload(t, p).SetEnabled("prod", false); err == nil {
		t.Fatal("want error")
	}
	st := reload(t, p)
	if st.Active.Contexts["prod-a"] == nil {
		t.Error("prod-a removed from active although the stash write failed")
	}
}

func TestEnabledGroupWithStashedContextIsDrift(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).SetEnabled("prod", false))
	// Simulate an interrupted enable: groups.yaml says enabled, contexts still stashed.
	st := reload(t, p)
	st.Groups.Groups["prod"].Enabled = true
	must(t, st.Groups.save(p.Groups))

	st = reload(t, p)
	if out := st.Status(); !strings.Contains(out, "prod-a (prod) is hidden in stash") {
		t.Errorf("status missing drift:\n%s", out)
	}
	must(t, st.SetEnabled("prod", true))
	if out := reload(t, p).Status(); strings.Contains(out, "Drift") {
		t.Errorf("drift remains:\n%s", out)
	}
}

func TestRowsSortedByContextName(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).Assign("lab", "stg")) // group changes must not reorder rows
	var got []string
	for _, r := range reload(t, p).Rows() {
		got = append(got, r.Group+"/"+r.Context)
	}
	eq(t, "rows", got, []string{"stg/lab", "prod/prod-a", "prod/prod-b", "stg/stg-a"})
}

func TestDeletedContextKeepsMembershipWhenReadded(t *testing.T) {
	p := setup(t)
	st := reload(t, p)
	ctx := st.Active.Contexts["prod-a"]
	delete(st.Active.Contexts, "prod-a") // kubectl config delete-context prod-a
	must(t, writeKubeconfig(p.Active, st.Active))
	must(t, reload(t, p).SetEnabled("stg", true)) // any unrelated write

	st = reload(t, p)
	st.Active.Contexts["prod-a"] = ctx // a credentials tool re-adds it
	must(t, writeKubeconfig(p.Active, st.Active))
	must(t, reload(t, p).SetEnabled("prod", false))
	if reload(t, p).Active.Contexts["prod-a"] != nil {
		t.Error("re-added prod-a still visible after disable prod")
	}
}

func TestUnknownGroup(t *testing.T) {
	if err := reload(t, setup(t)).SetEnabled("nope", false); err == nil {
		t.Error("want error")
	}
}

func TestDuplicateContextInGroupsRejected(t *testing.T) {
	p := setup(t)
	must(t, os.WriteFile(p.Groups, []byte("groups:\n  a: {contexts: [x]}\n  b: {contexts: [x]}\n"), 0o600))
	if _, err := load(p); err == nil || !strings.Contains(err.Error(), `"x"`) {
		t.Errorf("err = %v", err)
	}
}

func TestMultiPathKubeconfigRejected(t *testing.T) {
	t.Setenv("KUBECONFIG", "/a"+string(os.PathListSeparator)+"/b")
	if _, err := defaultPaths(); err == nil {
		t.Error("want error")
	}
}
