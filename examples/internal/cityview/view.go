// Package cityview adapts the experimental geographic scene to a reusable Tea pane.
package cityview

import (
	tea "charm.land/bubbletea/v2"
	"fmt"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"github.com/charmbracelet/x/ansi"
	"image/color"
	"math"
	"nimbleterminal.dev/ntmaps/internal/city"
	picture "nimbleterminal.dev/ntpicture"
	"nimbleterminal.dev/ntpicture/legacy"
)

type Model struct {
	Chart   *chart.Model
	Scene   city.Scene
	Label   string
	width   int
	Skipped int
}

func New(d city.Dataset, scene city.Scene, mode chart.RenderMode, medium picture.KittyMedium) (*Model, error) {
	c := chart.New(90, 28, chart.WithRenderMode(mode), chart.WithTerminalSession(legacy.DefaultTerminalSession()), chart.WithKittyMedium(medium), chart.WithAutoRotate(false), chart.WithBackground(cityBackground()), chart.WithAxes(chart.Axes{X: chart.Axis{Hidden: true}, Y: chart.Axis{Hidden: true}, Z: chart.Axis{Hidden: true}}))
	c.SetColorLegendVisible(false)
	c.SetSeries(citySeries{geometry(scene)})
	c.SetCamera(initialCamera(scene))
	if err := c.Err(); err != nil {
		c.Close()
		return nil, err
	}
	return &Model{c, scene, d.Name, 90, d.SkippedWays + d.SkippedRelations + len(scene.Issues)}, nil
}
func (m *Model) Init() tea.Cmd { return m.Chart.Init() }
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.SetSize(v.Width, v.Height)
	case tea.KeyPressMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Sequence(m.Chart.Dispose(), tea.Quit)
		case "0":
			return m, m.Chart.SetCamera(initialCamera(m.Scene))
		case "t":
			mode := chart.WebGPU
			if m.Chart.RequestedRenderMode() == chart.WebGPU {
				mode = chart.Software
			}
			return m, m.Chart.SetRenderMode(mode)
		case "w", "a", "s", "d", "e", "f":
			c := m.Chart.Camera()
			step := float32(8 * m.Scene.Scale())
			angle := c.Beta * math.Pi / 180
			// Translate the orbit target and eye together; this is inspection flight,
			// with no collision detection or automatic neighborhood loading yet.
			forwardX, forwardY := float32(-math.Cos(angle)), float32(-math.Sin(angle))
			rightX, rightY := float32(-math.Sin(angle)), float32(math.Cos(angle))
			switch v.String() {
			case "w":
				c.Target.X += forwardX * step
				c.Target.Y += forwardY * step
			case "s":
				c.Target.X -= forwardX * step
				c.Target.Y -= forwardY * step
			case "d":
				c.Target.X += rightX * step
				c.Target.Y += rightY * step
			case "a":
				c.Target.X -= rightX * step
				c.Target.Y -= rightY * step
			case "e":
				c.Target.Z += step
			case "f":
				c.Target.Z -= step
			}
			return m, m.Chart.SetCamera(c)
		}
	}
	_, cmd := m.Chart.Update(msg)
	return m, cmd
}
func (m *Model) View() tea.View {
	v := m.Chart.View()
	clip := func(s string) string { return ansi.Truncate(s, max(1, m.width), "") }
	renderer := m.Chart.RenderMode().String()
	if m.Chart.RequestedRenderMode() != m.Chart.RenderMode() {
		renderer += " (requested " + m.Chart.RequestedRenderMode().String() + ")"
	}
	s := m.Scene.Stats
	lines := []string{
		fmt.Sprintf("%s · %s · %d buildings · %d skipped · flat ground", m.Label, renderer, len(m.Scene.Buildings), m.Skipped),
		fmt.Sprintf("heights: orange %d recorded · blue %d from floors · gray %d defaults", s.Recorded, s.Levels, s.Default),
		"arrows look · WASD move · E/F rise/lower · +/- zoom · 0 reset · t renderer · g graphics · q quit",
		"© OpenStreetMap contributors · ODbL · experimental city viewer",
	}
	if err := m.Chart.Err(); err != nil {
		lines[0] += " · " + err.Error()
	}
	for _, line := range lines {
		v.Content += "\n" + clip(line)
	}
	v.AltScreen = false
	return v
}

func cityBackground() color.Color         { return color.RGBA{18, 27, 38, 255} }
func (m *Model) SetSize(w, h int) tea.Cmd { m.width = w; return m.Chart.SetSize(w, max(3, h-4)) }
func (m *Model) SetScene(d city.Dataset, s city.Scene) tea.Cmd {
	m.Scene, m.Label, m.Skipped = s, d.Name, d.SkippedWays+d.SkippedRelations+len(s.Issues)
	return tea.Batch(m.Chart.SetSeries(citySeries{geometry(s)}), m.Chart.SetCamera(initialCamera(s)))
}
func (m *Model) Suspend() tea.Cmd {
	c := m.Chart.Camera()
	c.AutoRotate = false
	return tea.Batch(m.Chart.SetCamera(c), m.Chart.SetRenderMode(chart.Wireframe))
}
func (m *Model) Resume(mode chart.RenderMode) tea.Cmd { return m.Chart.SetRenderMode(mode) }
func (m *Model) Dispose() tea.Cmd                     { return m.Chart.Dispose() }
func (m *Model) Close() error                         { return m.Chart.Close() }

// Center converts the camera target back to geographic coordinates. Looking or
// zooming does not change it; moving along the ground does.
func (m *Model) Center() city.Coordinate {
	c := m.Chart.Camera()
	k := m.Scene.Scale()
	return m.Scene.Projection.Unproject(float64(c.Target.X)/k+(m.Scene.Min.X+m.Scene.Max.X)/2, float64(c.Target.Y)/k+(m.Scene.Min.Y+m.Scene.Max.Y)/2)
}
