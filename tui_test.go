package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIChangeGroup(t *testing.T) {
	p := setup(t)
	st := reload(t, p)
	m := &model{paths: p, st: st, rows: st.Rows()}
	press := func(k tea.KeyType) { m.Update(tea.KeyMsg{Type: k}) }
	group := func() string { return reload(t, p).Groups.groupOf("lab") }

	// Cursor starts on "lab" (ungrouped); cycle is (ungrouped) → prod → stg → (ungrouped).
	press(tea.KeyRight)
	if g := group(); g != "prod" {
		t.Fatalf("→ from ungrouped = %q, want prod", g)
	}
	press(tea.KeyRight)
	press(tea.KeyRight)
	if g := group(); g != "" {
		t.Fatalf("→ wrap = %q, want ungrouped", g)
	}
	press(tea.KeyLeft)
	if g := group(); g != "stg" {
		t.Fatalf("← wrap = %q, want stg", g)
	}
	if m.rows[m.cursor].Context != "lab" {
		t.Errorf("cursor left lab: on %q", m.rows[m.cursor].Context)
	}

	// g opens the picker on the current group; ↑ selects prod.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	press(tea.KeyUp)
	press(tea.KeyEnter)
	if g := group(); g != "prod" {
		t.Fatalf("picker = %q, want prod", g)
	}

	// The last picker entry creates a new group.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	press(tea.KeyDown)
	press(tea.KeyDown)
	press(tea.KeyEnter)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("lab-new")})
	press(tea.KeyEnter)
	if g := group(); g != "lab-new" {
		t.Fatalf("new group = %q, want lab-new", g)
	}
}

func TestTUIMoveIntoDisabledGroupAndDeletedGroup(t *testing.T) {
	p := setup(t)
	must(t, reload(t, p).SetEnabled("prod", false))
	st := reload(t, p)
	m := &model{paths: p, st: st, rows: st.Rows()} // cursor on "lab"

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("l")})
	if reload(t, p).Stash.Contexts["lab"] == nil || m.rows[m.cursor].Context != "lab" {
		t.Fatal("lab should be hidden in disabled prod with the cursor still on it")
	}

	// Picker open, then prod is deleted on disk: selecting it must not recreate it.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	_ = m.View() // renders the picker
	st = reload(t, p)
	delete(st.Groups.Groups, "prod")
	must(t, st.Groups.save(p.Groups))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // picker cursor is on prod
	if reload(t, p).Groups.Groups["prod"] != nil {
		t.Error("deleted group prod was recreated")
	}
}
