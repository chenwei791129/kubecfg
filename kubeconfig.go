package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	api "k8s.io/client-go/tools/clientcmd/api"
)

// Paths locates the three files kubecfg manages.
type Paths struct {
	Active string // the kubeconfig kubectl reads
	Stash  string // kubeconfig holding Disabled contexts
	Groups string // groups.yaml
}

func defaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	active := filepath.Join(home, ".kube", "config")
	if env := os.Getenv("KUBECONFIG"); env != "" {
		var parts []string
		for _, p := range filepath.SplitList(env) {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) > 1 {
			return Paths{}, fmt.Errorf("KUBECONFIG lists %d files; kubecfg supports a single kubeconfig", len(parts))
		}
		if len(parts) == 1 {
			active = parts[0]
		}
	}
	dir := filepath.Join(home, ".config", "kubecfg")
	return Paths{Active: active, Stash: filepath.Join(dir, "stash.yaml"), Groups: filepath.Join(dir, "groups.yaml")}, nil
}

// State is everything kubecfg knows, loaded from disk.
type State struct {
	paths  Paths
	Active *api.Config
	Stash  *api.Config
	Groups *Groups
}

func load(p Paths) (*State, error) {
	active, err := loadKubeconfig(p.Active)
	if err != nil {
		return nil, err
	}
	stash, err := loadKubeconfig(p.Stash)
	if err != nil {
		return nil, err
	}
	groups, err := loadGroups(p.Groups)
	if err != nil {
		return nil, err
	}
	return &State{paths: p, Active: active, Stash: stash, Groups: groups}, nil
}

func loadKubeconfig(path string) (*api.Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return api.NewConfig(), nil
	}
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// SetEnabled reconciles a group: enabling moves its contexts from the Stash into
// the Active kubeconfig, disabling moves them the other way. Idempotent.
func (s *State) SetEnabled(group string, enabled bool) error {
	grp, ok := s.Groups.Groups[group]
	if !ok {
		return fmt.Errorf("group %q does not exist", group)
	}
	grp.Enabled = enabled
	return s.place(grp.Contexts, enabled)
}

// Assign puts ctx into group (or Ungrouped when group is "") and moves it to
// match the new group's state.
func (s *State) Assign(ctx, group string) error {
	s.Groups.assign(ctx, group)
	return s.place([]string{ctx}, group == "" || s.Groups.Groups[group].Enabled)
}

// place moves the named contexts into the Active kubeconfig (visible) or the
// Stash (hidden), then persists all files in a crash-safe order.
func (s *State) place(names []string, visible bool) error {
	for _, n := range names {
		if visible {
			restore(s.Active, s.Stash, n)
		} else {
			stash(s.Stash, s.Active, n)
		}
	}
	// Write the destination first and stop on failure, so a failed or
	// interrupted run duplicates entries but never loses them.
	first, second := s.writeActive, func() error { return writeKubeconfig(s.paths.Stash, s.Stash) }
	if !visible {
		first, second = second, first
	}
	if err := first(); err != nil {
		return err
	}
	if err := second(); err != nil {
		return err
	}
	// Membership of deleted contexts is kept on purpose: if a tool re-adds
	// the context later, it must still belong to its group.
	return s.Groups.save(s.paths.Groups)
}

// stash moves a context and copies of its cluster/user from active into st.
// Same-name entries in the Stash are overwritten: Active wins.
func stash(st, active *api.Config, name string) {
	c, ok := active.Contexts[name]
	if !ok {
		return
	}
	st.Contexts[name] = c
	if cl, ok := active.Clusters[c.Cluster]; ok {
		st.Clusters[c.Cluster] = cl
	}
	if u, ok := active.AuthInfos[c.AuthInfo]; ok {
		st.AuthInfos[c.AuthInfo] = u
	}
	delete(active.Contexts, name)
	if active.CurrentContext == name {
		active.CurrentContext = ""
	}
	dropIfUnreferenced(active, c.Cluster, c.AuthInfo)
}

// restore moves a context from st back into active. Existing same-name
// entries in active are kept: Active wins.
func restore(active, st *api.Config, name string) {
	c, ok := st.Contexts[name]
	if !ok {
		return
	}
	if _, exists := active.Contexts[name]; !exists {
		active.Contexts[name] = c
		if cl, ok := st.Clusters[c.Cluster]; ok && active.Clusters[c.Cluster] == nil {
			active.Clusters[c.Cluster] = cl
		}
		if u, ok := st.AuthInfos[c.AuthInfo]; ok && active.AuthInfos[c.AuthInfo] == nil {
			active.AuthInfos[c.AuthInfo] = u
		}
	}
	delete(st.Contexts, name)
	dropIfUnreferenced(st, c.Cluster, c.AuthInfo)
}

// dropIfUnreferenced removes a cluster/user that no remaining context uses.
func dropIfUnreferenced(cfg *api.Config, cluster, user string) {
	clusterUsed, userUsed := false, false
	for _, c := range cfg.Contexts {
		clusterUsed = clusterUsed || c.Cluster == cluster
		userUsed = userUsed || c.AuthInfo == user
	}
	if !clusterUsed {
		delete(cfg.Clusters, cluster)
	}
	if !userUsed {
		delete(cfg.AuthInfos, user)
	}
}

func (s *State) writeActive() error {
	if data, err := os.ReadFile(s.paths.Active); err == nil {
		if err := writeFileAtomic(s.paths.Active+".kubecfg.bak", data); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return writeKubeconfig(s.paths.Active, s.Active)
}

func writeKubeconfig(path string, cfg *api.Config) error {
	data, err := clientcmd.Write(*cfg)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic writes via a temp file + rename, following a symlinked target.
func writeFileAtomic(path string, data []byte) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".kubecfg-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Persist the rename so write ordering across directories survives power loss.
	// shortcut: Windows cannot fsync a read-only directory handle, so it skips this; add a
	// write-access directory open (like etcd's OpenDir) if power-loss safety matters there.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Row is one context as shown by status and the TUI.
type Row struct {
	Context string
	Group   string // "" = Ungrouped
	Visible bool   // present in the Active kubeconfig
	Drift   bool   // location disagrees with group state
}

// Rows lists every known context sorted by name, so rows stay put when a
// context changes group.
func (s *State) Rows() []Row {
	names := map[string]bool{}
	for n := range s.Active.Contexts {
		names[n] = true
	}
	for n := range s.Stash.Contexts {
		names[n] = true
	}
	var rows []Row
	for n := range names {
		g := s.Groups.groupOf(n)
		want := g == "" || s.Groups.Groups[g].Enabled
		vis := s.Active.Contexts[n] != nil
		rows = append(rows, Row{Context: n, Group: g, Visible: vis, Drift: vis != want})
	}
	slices.SortFunc(rows, func(a, b Row) int { return strings.Compare(a.Context, b.Context) })
	return rows
}

func (s *State) Status() string {
	var b strings.Builder
	rows := s.Rows()
	groups := s.Groups.names()
	count := map[string]int{}
	for _, r := range rows {
		count[r.Group]++
	}
	fmt.Fprintf(&b, "%-20s %-9s %s\n", "GROUP", "STATE", "CONTEXTS")
	for _, g := range groups {
		state := "disabled"
		if s.Groups.Groups[g].Enabled {
			state = "enabled"
		}
		fmt.Fprintf(&b, "%-20s %-9s %d\n", g, state, count[g])
	}
	var ungrouped, drift []string
	for _, r := range rows {
		if r.Group == "" && r.Visible {
			ungrouped = append(ungrouped, r.Context)
		}
		if r.Drift {
			where := "hidden in stash"
			if r.Visible {
				where = "visible in active kubeconfig"
			}
			label := r.Group
			if label == "" {
				label = "ungrouped"
			}
			drift = append(drift, fmt.Sprintf("  %s (%s) is %s", r.Context, label, where))
		}
	}
	if len(ungrouped) > 0 {
		fmt.Fprintf(&b, "\nUngrouped (always visible): %s\n", strings.Join(ungrouped, ", "))
	}
	if len(drift) > 0 {
		fmt.Fprintf(&b, "\nDrift (re-run enable/disable to fix):\n%s\n", strings.Join(drift, "\n"))
	}
	return b.String()
}
