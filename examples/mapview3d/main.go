// ntcharts-osm - Copyright (c) 2026 Neomantra Corp.

package main

import (
	"charm.land/bubbles/v2/key"
	"flag"
	"fmt"
	"github.com/NimbleMarkets/ntcharts-osm/examples/internal/demorun"
	"image"
	"nimbleterminal.dev/ntpicture/legacy"
	"os"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts-osm/mapview"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"github.com/charmbracelet/x/ansi"
	picture "nimbleterminal.dev/ntpicture"
)

type model struct {
	source        mapview.Model
	chart         *chart.Model
	places        []mapview.Place
	place         int
	textureSource image.Image
	err           error
	width         int
}

func newModel(places []mapview.Place, medium picture.KittyMedium, renderer chart.RenderMode) *model {
	// Keep a square map texture across terminal resizes. Camera changes only redraw 3D.
	source := mapview.NewWithConfig(mapview.Config{Cols: 64, Rows: 33, PanBuffer: 1})
	view := chart.New(80, 21, chart.WithRenderMode(renderer), chart.WithTerminalSession(legacy.DefaultTerminalSession()), chart.WithKittyMedium(medium),
		chart.WithAxes(chart.Axes{X: chart.Axis{Hidden: true}, Y: chart.Axis{Hidden: true}, Z: chart.Axis{Hidden: true}}))
	view.SetColorLegendVisible(false)
	view.SetCamera(initialCamera())
	m := &model{source: source, chart: view, places: places, width: 80}
	m.selectPlace(0)
	return m
}

func initialCamera() chart.Camera {
	c := chart.DefaultCamera()
	c.Alpha, c.Beta, c.Distance = 55, -90, 3.4
	c.Projection = chart.Perspective
	return c
}

func (m *model) selectPlace(index int) {
	m.place = (index + len(m.places)) % len(m.places)
	p := m.places[m.place]
	m.source.SetLatLng(p.Lat, p.Lon, p.Zoom)
	m.source.SetMarkers([]mapview.Marker{{Lat: p.Lat, Lng: p.Lon}})
}

func (m *model) Init() tea.Cmd { return tea.Batch(m.source.Init(), m.chart.Init()) }

func (m *model) syncTexture() tea.Cmd {
	img := m.source.Image()
	if img == nil || img == m.textureSource {
		return nil
	}
	texture, err := chart.NewTexture(img)
	if err != nil {
		m.err = err
		return nil
	}
	var cmd tea.Cmd
	if m.textureSource == nil {
		cmd = m.chart.SetSurface("map", mapSurface(texture, img.Bounds()))
	} else {
		cmd = m.chart.SetSeriesTexture("map", texture)
	}
	m.err = m.chart.Err()
	if m.err == nil {
		m.textureSource = img
	}
	return cmd
}

func mapSurface(texture *chart.Texture, bounds image.Rectangle) chart.Surface {
	aspect := float64(bounds.Dx()) / float64(bounds.Dy())
	return chart.Surface{NX: 2, NY: 2, Z: make([]float32, 4),
		MinX: -aspect, MaxX: aspect, MinY: -1, MaxY: 1,
		Material: &chart.Material{Texture: texture, Unlit: true}}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		return m, m.chart.SetSize(v.Width, max(3, v.Height-3))
	case tea.KeyPressMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Sequence(m.chart.Dispose(), tea.Quit)
		case "n", "p":
			delta := 1
			if v.String() == "p" {
				delta = -1
			}
			m.selectPlace(m.place + delta)
			return m, tea.Batch(m.source.Init(), m.syncTexture())
		case "0":
			return m, m.chart.SetCamera(initialCamera())
		}
		if key, ok := mapKey(v.String()); ok {
			var cmd tea.Cmd
			m.source, cmd = m.source.Update(key)
			return m, tea.Batch(cmd, m.syncTexture())
		}
		_, cmd := m.chart.Update(msg)
		return m, cmd
	}
	var sourceCmd tea.Cmd
	m.source, sourceCmd = m.source.Update(msg)
	_, chartCmd := m.chart.Update(msg)
	return m, tea.Batch(sourceCmd, chartCmd, m.syncTexture())
}

func mapKey(s string) (tea.KeyPressMsg, bool) {
	code, ok := map[string]rune{"w": tea.KeyUp, "a": tea.KeyLeft, "s": tea.KeyDown,
		"d": tea.KeyRight, "[": '-', "]": '+'}[s]
	key := tea.KeyPressMsg{Code: code}
	if code == '+' || code == '-' {
		key.Text = string(code)
	}
	return key, ok
}

func (m *model) View() tea.View {
	v := m.chart.View()
	lat, lng := m.source.Center()
	status := fmt.Sprintf("%s · %.5f, %.5f · map zoom %d · flat map surface", m.places[m.place].Name, lat, lng, m.source.Zoom())
	if m.source.Image() == nil {
		status = strings.Split(m.source.View().Content, "\n")[0]
	}
	if m.err != nil {
		status = m.err.Error()
	}
	clip := func(s string) string { return ansi.Truncate(s, max(1, m.width), "") }
	v.Content += "\n" + clip(status) + "\n" + clip("arrows tilt/rotate · WASD map pan · [] map zoom · +/- camera zoom · n/p place · 0 reset · q quit")
	v.Content += "\n" + clip("Map data © OpenStreetMap contributors · r auto-rotate · o projection · g glyph/Kitty")
	v.AltScreen = true
	return v
}

func main() {
	defaultMedium := "direct"
	if runtime.GOOS == "js" {
		defaultMedium = "shm"
	}
	mediumFlag := flag.String("medium", defaultMedium, "Kitty transport: direct or shm (local terminals)")
	renderFlag := flag.String("renderer", "software", "3D renderer: software or gpu")
	flag.Parse()
	medium := picture.KittyMediumDirect
	if *mediumFlag == "shm" {
		medium = picture.KittyMediumSharedMemory
	} else if *mediumFlag != "direct" {
		fmt.Fprintln(os.Stderr, "medium must be direct or shm")
		os.Exit(2)
	}
	renderer := chart.Software
	if *renderFlag == "gpu" {
		renderer = chart.WebGPU
	} else if *renderFlag != "software" {
		fmt.Fprintln(os.Stderr, "renderer must be software or gpu")
		os.Exit(2)
	}
	places, err := mapview.EmbeddedPlaces()
	if err != nil || len(places) == 0 {
		fmt.Fprintln(os.Stderr, "cannot load places:", err)
		os.Exit(1)
	}
	m := newModel(places, medium, renderer)
	err = demorun.Run(m, "mapview3d", "Explore a textured 3D map", demorun.Keys{
		Primary: []key.Binding{demorun.Binding("left", "Rotate left"), demorun.Binding("right", "Rotate right"), demorun.Binding("up", "Tilt up"), demorun.Binding("down", "Tilt down"), demorun.Binding("n", "Next place")},
		More:    []key.Binding{demorun.Binding("w", "Map north"), demorun.Binding("s", "Map south"), demorun.Binding("a", "Map west"), demorun.Binding("d", "Map east"), demorun.Binding("+", "Camera closer"), demorun.Binding("-", "Camera farther"), demorun.Binding("g", "Graphics")},
	})
	m.source.Close()
	closeErr := m.chart.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
