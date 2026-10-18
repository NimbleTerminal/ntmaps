package mapview

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCachedPictureRefreshAfterReplaceResizeAndClear(t *testing.T) {
	m := New(20, 11)
	m.pic.SetImage(newSolidImage(color.RGBA{R: 255, A: 255}))
	red := m.pic.View().Content
	m.pic.SetImage(newSolidImage(color.RGBA{B: 255, A: 255}))
	blue := m.pic.View().Content
	if red == blue || blue == "" {
		t.Fatal("replacement retained the cached image")
	}
	m.pic.SetSize(8, 4)
	small := m.pic.View().Content
	if small == blue {
		t.Fatal("resize retained the old presentation")
	}
	for _, line := range strings.Split(small, "\n") {
		if ansi.StringWidth(line) > 8 {
			t.Fatal("resized image exceeds requested width")
		}
	}
	m.pic.SetImage(nil)
	if m.pic.View().Content != "" {
		t.Fatal("clear retained the cached image")
	}
}
