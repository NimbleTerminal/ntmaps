// mapview shows the smallest complete host for the public 2D map widget.
// See ../explorer for the places browser and experimental 3D view.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"nimbleterminal.dev/ntmaps"
	"nimbleterminal.dev/ntmaps/examples/internal/demorun"
	"nimbleterminal.dev/ntmaps/tile/pmtiles"
)

type model struct {
	mapView ntmaps.Model
	width   int
}

func newModel(medium ntmaps.KittyMedium, lat, lon float64, zoom int) *model {
	mv := ntmaps.NewWithConfig(ntmaps.Config{Cols: 80, Rows: 23, PreferKitty: true, KittyMedium: medium})
	mv.SetLatLng(lat, lon, zoom)
	mv.SetMarkers([]ntmaps.Marker{{Lat: lat, Lng: lon}})
	return &model{mv, 80}
}
func (m *model) Init() tea.Cmd { return m.mapView.Init() }
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		return m, m.mapView.SetSize(v.Width, max(2, v.Height-1))
	case tea.KeyPressMsg:
		switch v.String() {
		case "q", "ctrl+c":
			var cleanup tea.Cmd
			if m.mapView.RenderMode() == ntmaps.RenderKitty {
				cleanup = m.mapView.SetRenderMode(ntmaps.RenderGlyph)
			}
			return m, tea.Sequence(cleanup, tea.Quit)
		case "g":
			mode := ntmaps.RenderKitty
			if m.mapView.RenderMode() == mode {
				mode = ntmaps.RenderGlyph
			}
			return m, m.mapView.SetRenderMode(mode)
		}
	}
	// Forward every runtime message so asynchronous tile results, terminal
	// capability discovery and picture completion messages reach their owner.
	var cmd tea.Cmd
	m.mapView, cmd = m.mapView.Update(msg)
	return m, cmd
}
func (m *model) View() tea.View {
	v := m.mapView.View()
	v.Content += "\n" + ansi.Truncate("arrows pan · +/- zoom · g glyph/Kitty · q quit", max(1, m.width), "")
	v.AltScreen = true
	return v
}
func main() {
	defaultMedium := "direct"
	if runtime.GOOS == "js" {
		defaultMedium = "shm"
	}
	mediumFlag := flag.String("medium", defaultMedium, "Kitty transport: direct or shm")
	lat := flag.Float64("lat", 40.7415, "map center latitude")
	lon := flag.Float64("lon", -73.989, "map center longitude")
	zoom := flag.Int("zoom", 16, "map zoom")
	archive := flag.String("pmtiles", "", "serve tiles from a local PMTiles archive instead of OpenStreetMap")
	flag.Parse()
	medium := ntmaps.KittyMediumDirect
	if *mediumFlag == "shm" {
		medium = ntmaps.KittyMediumSharedMemory
	} else if *mediumFlag != "direct" {
		fmt.Fprintln(os.Stderr, "medium must be direct or shm")
		os.Exit(2)
	}
	m := newModel(medium, *lat, *lon, *zoom)
	if *archive != "" {
		src, err := pmtiles.Open(*archive, pmtiles.Options{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer src.Close()
		m.mapView.SetTileSource(src)
	}
	err := demorun.Run(m, "mapview", "Simple 2D map widget", demorun.Keys{Primary: []key.Binding{demorun.Binding("up", "North"), demorun.Binding("down", "South"), demorun.Binding("left", "West"), demorun.Binding("right", "East"), demorun.Binding("+", "Zoom in"), demorun.Binding("-", "Zoom out")}, More: []key.Binding{demorun.Binding("g", "Graphics")}})
	m.mapView.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
