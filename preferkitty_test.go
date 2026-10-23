package ntmaps

import (
	"testing"

	picture "nimbleterminal.dev/ntpicture"
)

type unrelatedMsg struct{}

func TestPreferKittySwitchesOnceSupported(t *testing.T) {
	m := NewWithConfig(Config{Cols: 80, Rows: 24, PreferKitty: true})
	if m.RenderMode() != RenderGlyph {
		t.Fatalf("expected Glyph before the probe resolves, got %v", m.RenderMode())
	}

	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderGlyph {
		t.Fatalf("expected Glyph while capability is unknown, got %v", m.RenderMode())
	}

	// A probe timeout can land before a late terminal reply flips the
	// capability to Supported, so Unsupported must not end the wait.
	m.pic.SetKittyCapability(picture.KittyCapabilityUnsupported)
	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderGlyph {
		t.Fatalf("expected Glyph while unsupported, got %v", m.RenderMode())
	}

	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderKitty {
		t.Fatalf("expected Kitty once supported, got %v", m.RenderMode())
	}
}

func TestPreferKittyDoesNotOverrideExplicitMode(t *testing.T) {

	m := NewWithConfig(Config{Cols: 80, Rows: 24, PreferKitty: true})
	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m.SetRenderMode(RenderGlyph)
	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderGlyph {
		t.Fatalf("expected an explicit Glyph choice to stick, got %v", m.RenderMode())
	}

	m = NewWithConfig(Config{Cols: 80, Rows: 24, PreferKitty: true})
	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderKitty {
		t.Fatalf("expected Kitty after auto-switch, got %v", m.RenderMode())
	}
	m.SetRenderMode(RenderGlyph)
	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderGlyph {
		t.Fatalf("expected Glyph to stick after switching back, got %v", m.RenderMode())
	}
}

func TestDefaultConfigStaysGlyph(t *testing.T) {
	m := New(80, 24)
	m.pic.SetKittyCapability(picture.KittyCapabilitySupported)
	m, _ = m.Update(unrelatedMsg{})
	if m.RenderMode() != RenderGlyph {
		t.Fatalf("expected Glyph without PreferKitty, got %v", m.RenderMode())
	}
}
