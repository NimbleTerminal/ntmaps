package ntmaps

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestOverlayBoxBottomRight(t *testing.T) {
	cols, rows := 40, 10
	row := strings.Repeat("X", cols)
	content := strings.Repeat(row+"\n", rows-1) + row
	overlay := "+---+\n| L |\n+---+"

	got := overlayBox(content, cols, rows, overlay, lipgloss.Right, lipgloss.Bottom)
	lines := strings.Split(got, "\n")
	if len(lines) != rows {
		t.Fatalf("expected %d output lines, got %d", rows, len(lines))
	}
	for i, line := range lines[:rows-3] {
		if line != row {
			t.Fatalf("expected line %d untouched, got %q", i, line)
		}
	}
	for i, want := range strings.Split(overlay, "\n") {
		line := lines[rows-3+i]
		if !strings.HasSuffix(line, want) {
			t.Fatalf("expected line %d to end with %q, got %q", rows-3+i, want, line)
		}
		if w := lipgloss.Width(line); w != cols {
			t.Fatalf("expected line %d to stay %d cells wide, got %d", rows-3+i, cols, w)
		}
	}
}

// Glyph mode can emit rows narrower than the cell rectangle; the overlay
// must still land at its column rather than sliding left.
func TestOverlayBoxPadsShortLines(t *testing.T) {
	cols, rows := 20, 4
	content := "XX\n\n\n"
	overlay := "[L]"

	got := overlayBox(content, cols, rows, overlay, lipgloss.Right, lipgloss.Bottom)
	lines := strings.Split(got, "\n")
	last := lines[len(lines)-1]
	if w := lipgloss.Width(last); w != cols {
		t.Fatalf("expected last line %d cells wide, got %d (%q)", cols, w, last)
	}
	if !strings.HasSuffix(last, overlay) {
		t.Fatalf("expected last line to end with %q, got %q", overlay, last)
	}
}

// TestViewPutsLoadingBadgeInCorner pins that a refresh over an existing map
// shows the badge in the bottom-right corner, leaving the center visible.
func TestViewPutsLoadingBadgeInCorner(t *testing.T) {
	m := New(80, 24)
	m.setSource(newTestTiles(t).provider())
	img := newSolidImage(color.RGBA{R: 100, G: 200, B: 50, A: 255})
	m, _ = m.Update(mapImageMsg{gen: 0, img: img})
	if cmd := m.renderMapCmd(); cmd == nil {
		t.Fatal("expected renderMapCmd to return a Cmd")
	}
	if !m.inFlight() {
		t.Fatal("expected a render in flight")
	}

	picRows := m.picRows()
	lines := strings.Split(m.View().Content, "\n")
	if len(lines) < picRows {
		t.Fatalf("expected at least %d lines, got %d", picRows, len(lines))
	}
	body := lines[:picRows]

	badgeRow := ansi.Strip(body[picRows-2])
	if !strings.HasSuffix(badgeRow, "Loading… │") {
		t.Fatalf("expected badge at the right edge of row %d, got %q", picRows-2, badgeRow)
	}
	for i, line := range body[:picRows-3] {
		if strings.Contains(ansi.Strip(line), "Loading") {
			t.Fatalf("expected no badge above the bottom rows, found one on row %d", i)
		}
	}
}
