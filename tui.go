package main

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct {
	paths   Paths
	st      *State
	rows    []Row
	cursor  int
	picking bool     // choosing a group from the list
	options []string // groups shown by the picker
	pick    int      // picker cursor; len(options) = "new group"
	editing bool     // typing a new group name
	input   string   // group name being typed
	msg     string   // last error or notice
}

func runTUI(paths Paths, st *State) error {
	_, err := tea.NewProgram(&model{paths: paths, st: st, rows: st.Rows()}).Run()
	return err
}

// fresh reloads state from disk before a change, so edits made by other tools
// while the TUI is open are not overwritten.
func (m *model) fresh() bool {
	st, err := load(m.paths)
	if err != nil {
		m.msg = "error: " + err.Error()
		return false
	}
	m.st = st
	return true
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.picking {
		switch key.String() {
		case "up", "k":
			m.pick = max(m.pick-1, 0)
		case "down", "j":
			m.pick = min(m.pick+1, len(m.options))
		case "enter":
			m.picking = false
			if m.pick == len(m.options) {
				m.editing, m.input = true, ""
			} else if m.fresh() {
				if name := m.options[m.pick]; m.st.Groups.Groups[name] == nil {
					m.apply(fmt.Errorf("group %q no longer exists", name))
				} else {
					m.apply(m.st.Assign(m.rows[m.cursor].Context, name))
				}
			}
		case "esc", "q":
			m.picking = false
		}
		return m, nil
	}
	if m.editing {
		switch key.Type {
		case tea.KeyEnter:
			m.editing = false
			if name := strings.TrimSpace(m.input); name != "" && m.fresh() {
				m.apply(m.st.Assign(m.rows[m.cursor].Context, name))
			}
		case tea.KeyEsc:
			m.editing = false
		case tea.KeyBackspace:
			if r := []rune(m.input); len(r) > 0 {
				m.input = string(r[:len(r)-1])
			}
		case tea.KeyRunes:
			m.input += string(key.Runes)
		}
		return m, nil
	}
	switch key.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = max(min(m.cursor+1, len(m.rows)-1), 0)
	}
	if len(m.rows) == 0 {
		return m, nil
	}
	row := m.rows[m.cursor]
	switch key.String() {
	case "g":
		m.picking, m.options, m.msg = true, m.st.Groups.names(), ""
		m.pick = max(slices.Index(m.options, row.Group), 0)
	case "left", "h", "right", "l":
		// Cycle the context through (ungrouped) and every group, wrapping around.
		if m.fresh() {
			cycle := append([]string{""}, m.st.Groups.names()...)
			step := 1
			if k := key.String(); k == "left" || k == "h" {
				step = len(cycle) - 1
			}
			cur := m.st.Groups.groupOf(row.Context) // from disk, not the stale row
			next := cycle[(slices.Index(cycle, cur)+step)%len(cycle)]
			m.apply(m.st.Assign(row.Context, next))
		}
	case "u":
		if m.fresh() {
			m.apply(m.st.Assign(row.Context, ""))
		}
	case " ", "e":
		if row.Group == "" {
			m.msg = "ungrouped contexts are always enabled; press g to assign a group"
			break
		}
		if m.fresh() {
			if grp, ok := m.st.Groups.Groups[row.Group]; ok {
				m.apply(m.st.SetEnabled(row.Group, !grp.Enabled))
			} else {
				m.apply(fmt.Errorf("group %q no longer exists", row.Group))
			}
		}
	}
	return m, nil
}

// apply refreshes rows after a change, keeping the cursor on the same context.
func (m *model) apply(err error) {
	cur := m.rows[m.cursor].Context
	m.msg = ""
	if err != nil {
		m.fresh() // discard the in-memory change that failed to save
		m.msg = "error: " + err.Error()
	}
	m.rows = m.st.Rows()
	m.cursor = min(m.cursor, max(len(m.rows)-1, 0))
	for i, r := range m.rows {
		if r.Context == cur {
			m.cursor = i
		}
	}
}

func (m *model) View() string {
	var b strings.Builder
	b.WriteString("kubecfg — contexts\n\n")
	for i, r := range m.rows {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		group, state := r.Group, "enabled"
		if group == "" {
			group = "(ungrouped)"
		} else if !m.st.Groups.Groups[r.Group].Enabled {
			state = "disabled"
		}
		drift := ""
		if r.Drift {
			drift = "  ! drift"
		}
		fmt.Fprintf(&b, "%s%-14s %-9s %s%s\n", cursor, group, state, r.Context, drift)
	}
	b.WriteString("\n")
	switch {
	case m.picking:
		fmt.Fprintf(&b, "move %s to group:\n", m.rows[m.cursor].Context)
		for i, name := range append(m.options, "+ new group…") {
			cursor := "  "
			if i == m.pick {
				cursor = "> "
			}
			fmt.Fprintf(&b, "  %s%s\n", cursor, name)
		}
		b.WriteString("↑/↓ choose · enter select · esc cancel\n")
	case m.editing:
		fmt.Fprintf(&b, "new group name: %s█  (enter to save, esc to cancel)\n", m.input)
	default:
		b.WriteString("↑/↓ move · ←/→ change group · g pick group · u ungroup · space/e toggle group · q quit\n")
	}
	if m.msg != "" {
		b.WriteString(m.msg + "\n")
	}
	return b.String()
}
