// mapview shows the smallest complete host for the public 2D map widget.
// See ../explorer for the places browser and experimental 3D view.
package main

import (
	"context"
	"flag"
	"fmt"
	"image/png"
	"os"
	"runtime"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"nimbleterminal.dev/ntmaps"
	"nimbleterminal.dev/ntmaps/examples/internal/demorun"
	"nimbleterminal.dev/ntmaps/geodata"
	"nimbleterminal.dev/ntmaps/tile"
	"nimbleterminal.dev/ntmaps/tile/pmtiles"
)

type model struct {
	mapView ntmaps.Model
	width   int
	fit     bool // frame the file's places and tracks once the size is known
}

// newModel centers a marker at lat, lon, or, given a file's data, shows its
// places and tracks.
func newModel(medium ntmaps.KittyMedium, lat, lon float64, zoom int, data *geodata.Data, src tile.Source) *model {
	mv := ntmaps.NewWithConfig(ntmaps.Config{Source: src, UserAgent: demorun.UserAgent, Geocoder: demorun.Geocoder, Cols: 80, Rows: 23, PreferKitty: true, KittyMedium: medium})
	mv.SetLatLng(lat, lon, zoom)
	if data != nil {
		mv.SetMarkers(data.Markers())
		mv.SetPaths(data.Paths())
		return &model{mv, 80, true}
	}
	mv.SetMarkers([]ntmaps.Marker{{Lat: lat, Lng: lon}})
	return &model{mapView: mv, width: 80}
}
func (m *model) Init() tea.Cmd { return m.mapView.Init() }
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		cmd := m.mapView.SetSize(v.Width, max(2, v.Height-1))
		if m.fit && m.mapView.FitContent() {
			m.fit = false
			cmd = tea.Batch(cmd, m.mapView.Init())
		}
		return m, cmd
	case tea.KeyPressMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Sequence(m.mapView.Dispose(), tea.Quit)
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
	v.Content += "\n" + ansi.Truncate("arrows/drag pan · +/-/wheel zoom · g glyph/Kitty · q quit", max(1, m.width), "")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
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
	geo := flag.String("geodata", "", "show the places and tracks of a GeoJSON or GPX file")
	snapshot := flag.String("snapshot", "", "write a PNG of the map to this file and exit, no terminal needed")
	width := flag.Int("width", 1024, "snapshot width in pixels")
	height := flag.Int("height", 512, "snapshot height in pixels")
	flag.Parse()
	medium := ntmaps.KittyMediumDirect
	if *mediumFlag == "shm" {
		medium = ntmaps.KittyMediumSharedMemory
	} else if *mediumFlag != "direct" {
		fmt.Fprintln(os.Stderr, "medium must be direct or shm")
		os.Exit(2)
	}
	var src tile.Source = demorun.OpenStreetMap()
	if *archive != "" {
		a, err := pmtiles.Open(*archive, pmtiles.Options{})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer a.Close()
		src = a
	}
	var data *geodata.Data
	if *geo != "" {
		d, err := readGeodata(*geo)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		data = &d
	}
	if *snapshot != "" {
		if err := writeSnapshot(*snapshot, src, *lat, *lon, *zoom, *width, *height, data); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	m := newModel(medium, *lat, *lon, *zoom, data, src)
	err := demorun.Run(m, "mapview", "Simple 2D map widget", demorun.Keys{Primary: []key.Binding{demorun.Binding("up", "North"), demorun.Binding("down", "South"), demorun.Binding("left", "West"), demorun.Binding("right", "East"), demorun.Binding("+", "Zoom in"), demorun.Binding("-", "Zoom out")}, More: []key.Binding{demorun.Binding("g", "Graphics")}})
	m.mapView.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// writeSnapshot renders the map once with ntmaps.Render and saves it as
// PNG: the headless path, for scripts and documentation images.
func writeSnapshot(path string, src tile.Source, lat, lon float64, zoom, width, height int, data *geodata.Data) error {
	o := ntmaps.RenderOptions{
		UserAgent: demorun.UserAgent, Source: src, Lat: lat, Lng: lon, Zoom: zoom, Width: width, Height: height,
		Markers: []ntmaps.Marker{{Lat: lat, Lng: lon}},
	}
	if data != nil {
		o.Markers, o.Paths = data.Markers(), data.Paths()
		if data.Bounds.Valid() {
			o.Bounds = &data.Bounds
		} else if data.Points > 0 {
			o.Lat, o.Lng = data.Bounds.South, data.Bounds.West // One place, or a line due east.
		}
	}
	img, err := ntmaps.Render(context.Background(), o)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// readGeodata reads a GeoJSON or GPX file within geodata's default limits.
func readGeodata(path string) (geodata.Data, error) {
	f, err := os.Open(path)
	if err != nil {
		return geodata.Data{}, err
	}
	defer f.Close()
	return geodata.Read(f, geodata.Limits{})
}
