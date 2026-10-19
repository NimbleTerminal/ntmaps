// cityview3d is an experimental neighborhood viewer backed by internal/city.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image/png"
	"os"
	"os/signal"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/NimbleMarkets/ntcharts-osm/examples/internal/cityview"
	"github.com/NimbleMarkets/ntcharts-osm/examples/internal/demorun"
	"github.com/NimbleMarkets/ntcharts-osm/internal/city"
	chart "github.com/NimbleMarkets/ntcharts3d"
	picture "nimbleterminal.dev/ntpicture"
)

type model struct{ *cityview.Model }

func (m *model) View() tea.View { v := m.Model.View(); v.AltScreen = true; return v }
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := m.Model.Update(msg)
	return m, cmd
}

func load(file string, live bool, bounds city.Bounds) (city.Dataset, error) {
	if live {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		fmt.Fprintln(os.Stderr, "Loading one neighborhood from OpenStreetMap…")
		d, err := (city.Client{}).Fetch(ctx, bounds)
		d.Name = "OpenStreetMap neighborhood"
		return d, err
	}
	if file == "" {
		return cityview.Bundled()
	}
	var raw []byte
	name := file
	if file != "" {
		var err error
		f, e := os.Open(file)
		if e != nil {
			return city.Dataset{}, e
		}
		defer f.Close()
		raw, err = readSource(f)
		if err != nil {
			return city.Dataset{}, err
		}
		name = file
	}
	var metadata struct {
		Bounds city.Bounds `json:"bounds"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return city.Dataset{}, err
	}
	if metadata.Bounds.Validate() == nil {
		bounds = metadata.Bounds
	}
	d, err := city.Decode(bytes.NewReader(raw), bounds)
	d.Name = name
	return d, err
}
func run() error {
	file := flag.String("file", "", "local OSM Map/Overpass JSON file (optional bounds object)")
	live := flag.Bool("osm", false, "download one small OSM neighborhood instead of using bundled data")
	lat := flag.Float64("lat", 40.7415, "live/file center latitude")
	lon := flag.Float64("lon", -73.989, "live/file center longitude")
	radius := flag.Float64("radius", 200, "live/file neighborhood radius in meters (25..500)")
	renderer := flag.String("renderer", "gpu", "renderer: gpu or software")
	mediumFlag := flag.String("medium", "direct", "Kitty transport: direct or shm")
	snapshot := flag.String("snapshot", "", "save a 1280x720 PNG and exit without opening a terminal")
	flag.Parse()
	if *live && *file != "" {
		return fmt.Errorf("choose -osm or -file")
	}
	mode := chart.WebGPU
	if *renderer == "software" {
		mode = chart.Software
	} else if *renderer != "gpu" {
		return fmt.Errorf("renderer must be gpu or software")
	}
	medium := picture.KittyMediumDirect
	if *mediumFlag == "shm" {
		medium = picture.KittyMediumSharedMemory
	} else if *mediumFlag != "direct" {
		return fmt.Errorf("medium must be direct or shm")
	}
	bounds, err := city.Around(city.Coordinate{Lat: *lat, Lon: *lon}, *radius)
	if err != nil {
		return err
	}
	d, err := load(*file, *live, bounds)
	if err != nil {
		return err
	}
	scene, err := city.Build(d, city.DefaultHeightPolicy())
	if err != nil {
		return err
	}
	if len(scene.Buildings) == 0 {
		return fmt.Errorf("no supported building footprints in this source")
	}
	pane, err := cityview.New(d, scene, mode, medium)
	if err != nil {
		return err
	}
	m := &model{pane}
	defer m.Close()
	if *snapshot != "" {
		img, actual, err := m.Chart.Snapshot(1280, 720)
		if err != nil {
			return err
		}
		f, err := os.Create(*snapshot)
		if err != nil {
			return err
		}
		err = png.Encode(f, img)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintf(os.Stderr, "%s: %d buildings; renderer=%s (requested %s); %d skipped\n", *snapshot, len(scene.Buildings), actual, mode, m.Skipped)
		return nil
	}
	keys := demorun.Keys{Primary: []key.Binding{demorun.Binding("left", "Look left"), demorun.Binding("right", "Look right"), demorun.Binding("w", "Move forward"), demorun.Binding("s", "Move back"), demorun.Binding("t", "Renderer")}, More: []key.Binding{demorun.Binding("a", "Move left"), demorun.Binding("d", "Move right"), demorun.Binding("e", "Rise"), demorun.Binding("f", "Lower"), demorun.Binding("0", "Reset")}}
	return demorun.Run(m, "cityview3d", "Explore a 3D neighborhood", keys)
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
