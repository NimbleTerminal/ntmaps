package ntmaps

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	picture "nimbleterminal.dev/ntpicture"
)

func TestPictureRefreshAfterReplaceResizeAndClear(t *testing.T) {
	m := New(20, 11)
	drainMap(t, &m, m.pic.SetImage(newSolidImage(color.RGBA{R: 255, A: 255})))
	red := m.pic.View()
	drainMap(t, &m, m.pic.SetImage(newSolidImage(color.RGBA{B: 255, A: 255})))
	blue := m.pic.View()
	if red == blue || blue == "" {
		t.Fatal("replacement retained the cached image")
	}
	drainMap(t, &m, m.pic.SetSize(8, 4))
	small := m.pic.View()
	if small == blue {
		t.Fatal("resize retained the old presentation")
	}
	for _, line := range strings.Split(small, "\n") {
		if ansi.StringWidth(line) > 8 {
			t.Fatal("resized image exceeds requested width")
		}
	}
	drainMap(t, &m, m.pic.SetImage(nil))
	if m.pic.View() != "" {
		t.Fatal("clear retained the cached image")
	}
}

// drainMap executes Bubble Tea batches/sequences and routes runtime messages as
// a host using IsMapUpdate would. Raw terminal output is collected for assertions.
func drainMap(t *testing.T, m *Model, cmd tea.Cmd) string {
	t.Helper()
	var output string
	var run func(tea.Cmd)
	steps := 0
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		steps++
		if steps > 1000 {
			t.Fatal("command cycle did not settle")
		}
		msg := cmd()
		if msg == nil {
			return
		}
		v := reflect.ValueOf(msg)
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				run(v.Index(i).Interface().(tea.Cmd))
			}
			return
		}
		if raw, ok := msg.(tea.RawMsg); ok {
			output += fmt.Sprint(raw.Msg)
			return
		}
		if !IsMapUpdate(msg) {
			t.Fatalf("unroutable command result %T", msg)
		}
		var next tea.Cmd
		*m, next = m.Update(msg)
		run(next)
	}
	run(cmd)
	return output
}

func TestHostSessionAndImageIDs(t *testing.T) {
	session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported})
	defer session.Close()
	// A host starts discovery once; the map must not create another session.
	_ = session.Init()
	ids := 0
	m := NewWithConfig(Config{Cols: 8, Rows: 5, Session: session, NextImageID: func() (int, error) { ids++; return 4200 + ids, nil }})
	defer m.Close()
	if m.pic.Session() != session || m.pic.Init() != nil {
		t.Fatal("map did not reuse host discovery")
	}
	m.pic.SetMode(RenderKitty)
	out := drainMap(t, &m, m.presentImage(newSolidImage(color.RGBA{R: 255, A: 255})))
	if ids != 1 || !strings.Contains(out, "i=4201") {
		t.Fatalf("host allocator was not used: ids=%d output=%q", ids, out)
	}
	out = drainMap(t, &m, m.Dispose())
	if !strings.Contains(out, "a=d") || !strings.Contains(out, "i=4201") {
		t.Fatalf("Dispose did not delete map image: %q", out)
	}
	m.Close()
	m.Close()
	// Closing a borrower must leave the shared session usable.
	session.SetKittyCapability(picture.KittyCapabilityUnsupported)
	if session.Snapshot().Kitty != picture.KittyCapabilityUnsupported {
		t.Fatal("map closed the host session")
	}
}

func TestKeepRetainsFrameAndCloseIsIdempotent(t *testing.T) {
	m := NewWithConfig(Config{Cols: 8, Rows: 5, NextImageID: func() (int, error) { return 4301, nil }})
	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m.pic.SetMode(RenderKitty)
	drainMap(t, &m, m.presentImage(newSolidImage(color.RGBA{B: 255, A: 255})))
	before := m.View().Content
	keep := m.Keep()
	m.Close()
	m.Close()
	out := drainMap(t, &m, keep)
	if strings.Contains(out, "a=d") || !strings.Contains(out, "i=4301") {
		t.Fatalf("Keep must recreate the retained frame on the main screen: %q", out)
	}
	if m.View().Content != before || !m.pic.Closed() {
		t.Fatal("Keep lost the last view")
	}
	if m.Dispose() != nil {
		t.Fatal("Dispose after Keep must not delete retained image")
	}
}

func TestPendingPresentationSurvivesResizeAndModeChange(t *testing.T) {
	for _, mode := range []RenderMode{RenderGlyph, RenderKitty} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			m := animatedTestModel()
			m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
			m.pic.SetMode(mode)
			first := m.renderMapCmd()
			for i := 0; i < 10; i++ {
				m.SetLatLng(0, float64(i), 15)
				m.renderMapCmd()
			}
			resize := m.SetSize(12, 7)
			modeChange := m.SetRenderMode(RenderGlyph)
			drainMap(t, &m, first)
			drainMap(t, &m, resize)
			drainMap(t, &m, modeChange)
			if m.pic.Status().Busy || m.panState.pending != nil || m.pic.Status().Generation != m.pic.Status().Presented {
				t.Fatalf("queue stalled: %+v", m.pic.Status())
			}
		})
	}
}

func TestClearAndDisposeDropPendingPresentation(t *testing.T) {
	for _, dispose := range []bool{false, true} {
		m := animatedTestModel()
		first := m.renderMapCmd()
		m.panPixels(16, 0)
		m.renderMapCmd()
		var stop tea.Cmd
		if dispose {
			stop = m.Dispose()
		} else {
			m, stop = m.Update(MapCoordinates{Err: errExample{}})
		}
		drainMap(t, &m, stop)
		drainMap(t, &m, first)
		if m.panState.pending != nil || m.pic.View() != "" || m.pic.Status().Busy {
			t.Fatal("cleared pending image reappeared")
		}
		m.Close()
	}
}

func TestNewestPendingPixelsWin(t *testing.T) {
	m := animatedTestModel()
	first := m.presentImage(newSolidImage(color.RGBA{R: 255, A: 255}))
	for i := 0; i < 20; i++ {
		m.presentImage(newSolidImage(color.RGBA{G: uint8(i), A: 255}))
	}
	newest := newSolidImage(color.RGBA{B: 255, A: 255})
	m.presentImage(newest)
	drainMap(t, &m, first)
	want := animatedTestModel()
	drainMap(t, &want, want.presentImage(newest))
	if m.pic.View() != want.pic.View() {
		t.Fatal("presentation did not use newest pixels")
	}
	m.Close()
	want.Close()
}

func TestImageIDFailureDoesNotStallPendingFrame(t *testing.T) {
	calls := 0
	m := NewWithConfig(Config{Cols: 8, Rows: 5, NextImageID: func() (int, error) {
		calls++
		if calls == 2 {
			return 0, fmt.Errorf("allocation failed")
		}
		return 4400 + calls, nil
	}})
	defer m.Close()
	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m.pic.SetMode(RenderKitty)
	first := m.presentImage(newSolidImage(color.RGBA{R: 255, A: 255}))
	m.presentImage(newSolidImage(color.RGBA{G: 255, A: 255}))
	drainMap(t, &m, first)
	if m.pic.Status().Busy || m.panState.pending != nil || m.pic.Status().Err == nil {
		t.Fatalf("allocation error did not settle: %+v", m.pic.Status())
	}
	drainMap(t, &m, m.presentImage(newSolidImage(color.RGBA{B: 255, A: 255})))
	if m.pic.Status().Busy || m.pic.Status().Err != nil || m.pic.Status().Presented != m.pic.Status().Generation {
		t.Fatalf("presentation did not recover: %+v", m.pic.Status())
	}
}
