package main

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"nimbleterminal.dev/ntmaps/internal/city"
	picture "nimbleterminal.dev/ntpicture"
)

func fixture(t *testing.T) *model {
	t.Helper()
	l, err := newLoader("", true)
	if err != nil {
		t.Fatal(err)
	}
	m := initialModel(0, l, chart.Software)
	t.Cleanup(m.Close)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	return m
}
func press(s string) tea.KeyPressMsg { return tea.KeyPressMsg{Code: rune(s[0]), Text: s} }

// Execute only the loader branch of a Sequence. Tile HTTP and render commands
// are intentionally not executed in state-transition tests.
func loadReply(t *testing.T, cmd tea.Cmd) cityLoaded {
	t.Helper()
	for i := 0; i < 10; i++ {
		msg := cmd()
		if v, ok := msg.(cityLoaded); ok {
			return v
		}
		value := reflect.ValueOf(msg)
		if value.Kind() != reflect.Slice || value.Len() == 0 {
			t.Fatalf("no load branch: %T", msg)
		}
		cmd = value.Index(value.Len() - 1).Interface().(tea.Cmd)
	}
	t.Fatal("load command did not terminate")
	return cityLoaded{}
}
func enterCity(t *testing.T, m *model) {
	t.Helper()
	_, cmd := m.Update(press("v"))
	reply := loadReply(t, cmd)
	m.Update(reply)
	if m.loadErr != nil || m.loading || m.cityPane == nil {
		t.Fatalf("city load failed: %v", m.loadErr)
	}
}
func TestSwitchingPreservesGroundLocationAndReusesScene(t *testing.T) {
	m := fixture(t)
	lat, lon := m.mv.Center()
	enterCity(t, m)
	if m.mv.RenderMode() != 0 {
		t.Fatal("inactive 2D picture still Kitty")
	}
	pane := m.cityPane
	center := pane.Center()
	if math.Abs(center.Lat-lat) > 1e-6 || math.Abs(center.Lon-lon) > 1e-6 {
		t.Fatal("3D did not start at 2D center")
	}
	m.Update(press("w"))
	moved := pane.Center()
	if moved == center {
		t.Fatal("city did not move")
	}
	m.Update(press("v"))
	a, b := m.mv.Center()
	if math.Abs(a-moved.Lat) > 1e-7 || math.Abs(b-moved.Lon) > 1e-7 {
		t.Fatal("3D center did not reach 2D map")
	}
	m.Update(press("v"))
	if m.loading || m.cityPane != pane {
		t.Fatal("nearby view switch fetched/recreated geometry")
	}
}
func TestLateLoadsAndLeavingWhileLoading(t *testing.T) {
	m := fixture(t)
	enterCity(t, m)
	old := m.cityPane
	m.list.Select(1)
	m.syncMapToSelection()
	generation := m.request
	lat, lon := m.mv.Center()
	if !m.loading {
		t.Fatal("new place did not load")
	}
	m.Update(press("v"))
	a, b := m.mv.Center()
	if a != lat || b != lon {
		t.Fatal("leaving pending load restored the old city's center")
	}
	m.Update(cityLoaded{generation: generation, key: "old", err: errors.New("late")})
	if m.loadErr != nil || m.in3D || m.cityPane != old {
		t.Fatal("late result changed inactive view")
	}
	m.Update(press("v"))
	current := m.request
	m.Update(cityLoaded{generation: current - 1, err: errors.New("stale")})
	if m.loadErr != nil || !m.loading {
		t.Fatal("stale result replaced current request")
	}
	m.Update(cityLoaded{generation: current, err: errors.New("offline")})
	if !strings.Contains(m.View().Content, "Could not load") {
		t.Fatal("load failure is invisible")
	}
}
func TestFilteringAndLayout(t *testing.T) {
	m := fixture(t)
	m.Update(press("/"))
	m.Update(press("v"))
	if m.in3D || m.list.FilterState() != list.Filtering {
		t.Fatal("filter typing changed view")
	}
	m.list.ResetFilter()
	for _, size := range [][2]int{{100, 40}, {100, 20}, {80, 24}, {60, 20}, {120, 30}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		v := m.View().Content
		if lipgloss.Width(v) > size[0] || lipgloss.Height(v) > size[1] {
			L := m.layout()

			t.Fatalf("view %dx%d exceeds %v; layout=%+v list=%d detail=%d map=%d help=%d", lipgloss.Width(v), lipgloss.Height(v), size, L, lipgloss.Height(m.list.View()), lipgloss.Height(m.detailView(L.leftInnerW, L.detailInnerH)), lipgloss.Height(m.mv.View().Content), lipgloss.Height(m.help.View(m.keys)))
		}
	}
}
func TestReplacementCancelsAndShutdownRejectsLateLoad(t *testing.T) {
	m := fixture(t)
	started := make(chan context.Context, 1)
	m.loader.Fetch = func(ctx context.Context, b city.Bounds) (city.Dataset, error) {
		started <- ctx
		<-ctx.Done()
		return city.Dataset{}, ctx.Err()
	}
	m.in3D = true
	m.mv.SetLatLng(10, 10, 16)
	cmd := m.requestCity(false)
	done := make(chan cityLoaded, 1)
	go func() { done <- loadReply(t, cmd) }()
	var first context.Context
	select {
	case first = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch did not start")
	}
	if first.Err() != nil {
		t.Fatal("request canceled before replacement")
	}
	m.requestCity(false)
	var reply cityLoaded
	select {
	case reply = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("replacement did not cancel active fetch")
	}
	if first.Err() == nil {
		t.Fatal("active request was not canceled")
	}
	m.Update(press("q"))
	m.Update(reply)
	if !m.closed || m.cityPane != nil {
		t.Fatal("late result reopened closed app")
	}
}
func TestSharedTerminalSessionNotClosedByCity(t *testing.T) {
	for _, term := range []string{"dumb", "xterm-kitty"} {
		t.Run(term, func(t *testing.T) {
			for _, key := range []string{"TERM_PROGRAM", "KITTY_WINDOW_ID", "KITTY_INSTALLATION_DIR", "GHOSTTY_RESOURCES_DIR", "WEZTERM_EXECUTABLE", "WEZTERM_PANE", "TMUX"} {
				t.Setenv(key, "")
			}
			t.Setenv("TERM", term)
			m := fixture(t)
			enterCity(t, m)
			// Opening the city initializes discovery and may legitimately change
			// capability on a headless runner. Only Close must preserve this state.
			before := m.terminal.Snapshot()
			m.cityPane.Close()
			if after := m.terminal.Snapshot(); before != after {
				t.Fatalf("closing city altered shared terminal state: before=%+v after=%+v", before, after)
			}
			// Close leaves the snapshot intact even when it closes a session, so
			// also prove that the host can still update it and receive notifications.
			capability := picture.KittyCapabilitySupported
			if before.Kitty == capability {
				capability = picture.KittyCapabilityUnsupported
			}
			cmd := m.terminal.SetKittyCapability(capability)
			if cmd == nil {
				t.Fatal("shared terminal session stopped accepting updates")
			}
			msg, ok := cmd().(picture.TerminalSessionMsg)
			if !ok || msg.Session != m.terminal || msg.State.Kitty != capability {
				t.Fatalf("shared terminal session did not notify host: %#v", msg)
			}
		})
	}
}
