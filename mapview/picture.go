package mapview

import (
	"image"

	tea "charm.land/bubbletea/v2"
	"nimbleterminal.dev/ntpicture/legacy"
)

// cachedPicture preserves View's cache across ntpicture v0.1.0's legacy
// bridge, which renders a copy and discards its populated glyph cache.
// Remove this adapter once the upstream bridge retains View mutations.
// All mutations used by mapview invalidate the cached presentation.
type cachedPicture struct {
	legacy.Model
	view  tea.View
	valid bool
}

func (p *cachedPicture) View() tea.View {
	if !p.valid {
		p.view = p.Model.View()
		p.valid = true
	}
	return p.view
}
func (p *cachedPicture) SetImage(img image.Image) tea.Cmd {
	p.valid = false
	return p.Model.SetImage(img)
}
func (p *cachedPicture) SetSize(cols, rows int) tea.Cmd {
	p.valid = false
	return p.Model.SetSize(cols, rows)
}
func (p *cachedPicture) SetFit(fit FitMode) tea.Cmd {
	p.valid = false
	return p.Model.SetFit(fit)
}
func (p *cachedPicture) Toggle() tea.Cmd {
	p.valid = false
	return p.Model.Toggle()
}
func (p *cachedPicture) Update(msg tea.Msg) tea.Cmd {
	if legacy.IsPictureMsg(msg) {
		p.valid = false
	}
	return p.Model.Update(msg)
}
