package demorun

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
)

type testModel struct{ keys int }

func (*testModel) Init() tea.Cmd { return nil }
func (m *testModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyPressMsg); ok {
		m.keys++
	}
	return m, nil
}
func (*testModel) View() tea.View { return tea.NewView("map viewer") }

func TestOversizedActionsDoNotBlockDemo(t *testing.T) {
	inner := &testModel{}
	var diagnostics []error
	keys := Keys{}
	for ch := 'a'; ch <= 'z'; ch++ {
		keys.More = append(keys.More, Binding(string(ch), "Map control "+strings.Repeat("地图", 4)))
	}
	wrapped, err := wrap(inner, "mapview3d", "Explore the map", keys, func(err error) { diagnostics = append(diagnostics, err) })
	if err != nil {
		t.Fatalf("optional metadata blocked startup: %v", err)
	}
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Error(), "2048") {
		t.Fatalf("missing size diagnostic: %v", diagnostics)
	}
	wrapped.Init()
	wrapped.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	wrapped.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if inner.keys != 2 || wrapped.View().Content != "map viewer" {
		t.Fatal("fallback did not preserve keyboard/view behavior")
	}
	if len(diagnostics) != 1 {
		t.Fatal("repeated metadata failure spammed diagnostics")
	}
}

func TestFallbackStillRejectsInvalidModel(t *testing.T) {
	if _, err := wrap(nil, "mapview", "map", Keys{}, func(error) {}); err == nil {
		t.Fatal("nil model accepted")
	}
}
