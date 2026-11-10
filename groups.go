package main

import (
	"errors"
	"fmt"
	"os"
	"slices"

	"sigs.k8s.io/yaml"
)

// Group is a named set of contexts that is enabled or disabled as a unit.
type Group struct {
	Enabled  bool     `json:"enabled"`
	Contexts []string `json:"contexts"`
}

// Groups is the on-disk shape of groups.yaml.
type Groups struct {
	Groups map[string]*Group `json:"groups"`
}

func loadGroups(path string) (*Groups, error) {
	g := &Groups{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := yaml.UnmarshalStrict(data, g); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if g.Groups == nil {
		g.Groups = map[string]*Group{}
	}
	owner := map[string]string{}
	for name, grp := range g.Groups {
		if grp == nil {
			g.Groups[name] = &Group{} // like a missing `enabled` key: disabled
			continue
		}
		for _, c := range grp.Contexts {
			if prev, ok := owner[c]; ok {
				return nil, fmt.Errorf("%s: context %q is in both group %q and %q", path, c, prev, name)
			}
			owner[c] = name
		}
	}
	return g, nil
}

func (g *Groups) save(path string) error {
	data, err := yaml.Marshal(g)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// names returns the group names in sorted order.
func (g *Groups) names() []string {
	names := make([]string, 0, len(g.Groups))
	for n := range g.Groups {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// groupOf returns the group a context belongs to, or "" if Ungrouped.
func (g *Groups) groupOf(ctx string) string {
	for name, grp := range g.Groups {
		if slices.Contains(grp.Contexts, ctx) {
			return name
		}
	}
	return ""
}

// assign moves ctx into group name, creating it (enabled) if needed. An empty name ungroups ctx.
func (g *Groups) assign(ctx, name string) {
	if cur := g.groupOf(ctx); cur != "" {
		grp := g.Groups[cur]
		grp.Contexts = slices.DeleteFunc(grp.Contexts, func(c string) bool { return c == ctx })
	}
	if name == "" {
		return
	}
	grp, ok := g.Groups[name]
	if !ok {
		grp = &Group{Enabled: true}
		g.Groups[name] = grp
	}
	grp.Contexts = append(grp.Contexts, ctx)
	slices.Sort(grp.Contexts)
}
