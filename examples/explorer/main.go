package main

import (
	"bytes"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
	"nimbleterminal.dev/ntmaps"
	"nimbleterminal.dev/ntmaps/examples/internal/cityview"
	"nimbleterminal.dev/ntmaps/examples/internal/demorun"
	"nimbleterminal.dev/ntmaps/examples/internal/neighborhood"
	curated "nimbleterminal.dev/ntmaps/examples/internal/places"
	"nimbleterminal.dev/ntmaps/internal/city"
	"nimbleterminal.dev/ntmaps/tile"
	"nimbleterminal.dev/ntmaps/tile/pmtiles"
)

//go:embed flatiron.pmtiles
var flatironArchive []byte

// styles are the base maps the s key cycles through.
var styles = []struct {
	name string
	src  func() tile.Provider
}{
	{"OSM", tile.OpenStreetMap},
	{"Light", tile.CartoLight},
	{"Dark", tile.CartoDark},
	{"Topo", tile.OpenTopoMap},
	{"Satellite", tile.ArcGISWorldImagery},
}

// routes are paths drawn for some places: a walk around Flatiron, the
// LHC ring at CERN, and the prime meridian at Greenwich.
func routes() map[string][]ntmaps.Point {
	var ring []ntmaps.Point
	const lat, lng, radius = 46.2350, 6.0530, 4300.0
	for i := 0; i <= 64; i++ {
		a := 2 * math.Pi * float64(i) / 64
		ring = append(ring, ntmaps.Point{
			Lat: lat + radius*math.Cos(a)/111320,
			Lng: lng + radius*math.Sin(a)/(111320*math.Cos(lat*math.Pi/180)),
		})
	}
	return map[string][]ntmaps.Point{
		"Flatiron district":           {{Lat: 40.7420, Lng: -73.9897}, {Lat: 40.7411, Lng: -73.9897}, {Lat: 40.7404, Lng: -73.9881}, {Lat: 40.7415, Lng: -73.9872}, {Lat: 40.7425, Lng: -73.9885}, {Lat: 40.7420, Lng: -73.9897}},
		"CERN":                        ring,
		"Royal Observatory Greenwich": {{Lat: 51.4700, Lng: 0}, {Lat: 51.4850, Lng: 0}},
	}
}

func fitName(f ntmaps.FitMode) string {
	switch f {
	case ntmaps.FitFill:
		return "Fill"
	case ntmaps.FitCover:
		return "Cover"
	default:
		return "Contain"
	}
}

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
)

// placeItem adapts curated.Place to the bubbles/list DefaultItem interface.
type placeItem struct{ p curated.Place }

func (i placeItem) Title() string {
	if i.p.Recommended {
		return "★ " + i.p.Name
	}
	return i.p.Name
}
func (i placeItem) Description() string { return i.p.Description }
func (i placeItem) FilterValue() string { return i.p.Name }

// mapKeyMap binds the map to arrow keys; zoom stays on +/-.
func mapKeyMap() ntmaps.KeyMap {
	return ntmaps.KeyMap{
		Up:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑", "up")),
		Right:   key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "right")),
		Down:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↓", "down")),
		Left:    key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "left")),
		ZoomIn:  key.NewBinding(key.WithKeys("+", "="), key.WithHelp("+", "zoom in")),
		ZoomOut: key.NewBinding(key.WithKeys("-", "_"), key.WithHelp("-", "zoom out")),
	}
}

// listKeyMap leaves arrow keys for the map; list cursor uses shift+up/down + j/k.
func listKeyMap() list.KeyMap {
	km := list.DefaultKeyMap()
	km.CursorUp = key.NewBinding(key.WithKeys("shift+up", "k"), key.WithHelp("⇧↑/k", "up"))
	km.CursorDown = key.NewBinding(key.WithKeys("shift+down", "j"), key.WithHelp("⇧↓/j", "down"))
	km.PrevPage = key.NewBinding(key.WithKeys("pgup"), key.WithHelp("pgup", "prev page"))
	km.NextPage = key.NewBinding(key.WithKeys("pgdown"), key.WithHelp("pgdn", "next page"))
	km.GoToStart = key.NewBinding(key.WithKeys("home"), key.WithHelp("home", "first"))
	km.GoToEnd = key.NewBinding(key.WithKeys("end"), key.WithHelp("end", "last"))
	km.Quit = key.NewBinding() // disabled — top level handles q / ctrl+c
	return km
}

// appKeys collects the bindings shown in the bottom help line. Most map and
// list keys are owned by the embedded models; this struct only exists so the
// help bubble has a single KeyMap to render from.
type appKeys struct {
	in3D          bool
	view, refresh key.Binding
	pan           key.Binding
	zoom          key.Binding
	optZoom       key.Binding
	listNav       key.Binding
	filter        key.Binding
	mode          key.Binding
	style         key.Binding
	overlay       key.Binding
	fit           key.Binding
	frame         key.Binding
	search        key.Binding
	help          key.Binding
	quit          key.Binding
}

func newAppKeys() appKeys {
	return appKeys{
		view: demorun.Binding("v", "2D/3D"), refresh: demorun.Binding("R", "refresh 3D"),
		pan:     key.NewBinding(key.WithKeys("up", "down", "left", "right"), key.WithHelp("←↑↓→", "pan")),
		zoom:    key.NewBinding(key.WithKeys("+", "-", "=", "_"), key.WithHelp("+/-", "zoom")),
		optZoom: key.NewBinding(key.WithKeys("[", "]"), key.WithHelp("[/]", "optical")),
		listNav: key.NewBinding(key.WithKeys("shift+up", "shift+down", "j", "k"), key.WithHelp("⇧↑↓/jk", "list")),
		filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		mode:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "mode")),
		style:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "style")),
		overlay: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "seamarks")),
		fit:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fit")),
		frame:   key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "frame route")),
		search:  key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "go to")),
		help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more")),
		quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k appKeys) ShortHelp() []key.Binding {
	if k.in3D {
		return []key.Binding{k.view, demorun.Binding("arrows", "look"), demorun.Binding("WASD", "move"), demorun.Binding("e/f", "rise/lower"), demorun.Binding("t", "GPU/CPU"), k.listNav, k.help, k.quit}
	}
	return []key.Binding{k.view, k.pan, k.zoom, k.search, k.listNav, k.filter, k.mode, k.help, k.quit}
}
func (k appKeys) FullHelp() [][]key.Binding {
	if k.in3D {
		return [][]key.Binding{k.ShortHelp(), {k.refresh, demorun.Binding("0", "reset"), k.mode}}
	}
	return [][]key.Binding{k.ShortHelp(), {k.optZoom, k.style, k.overlay, k.fit, k.frame}}
}

type model struct {
	pointer               pointerState
	zones                 *zone.Manager
	itemPrefix            string
	cityPane              *cityview.Model
	loader                *neighborhood.Loader
	medium                ntmaps.KittyMedium
	renderer              chart.RenderMode
	mapMode               ntmaps.RenderMode
	in3D, loading, closed bool
	loadErr               error
	request               uint64
	cancel                context.CancelFunc
	cityKey               string
	cached                bool
	mv                    ntmaps.Model
	style                 int
	archive               *pmtiles.Archive
	routes                map[string][]ntmaps.Point
	search                textinput.Model
	searching             bool
	places                []curated.Place
	list                  list.Model
	help                  help.Model
	keys                  appKeys
	lastPlace             curated.Place // most recently auto-applied selection
	width, height         int
}

func initialModel(medium ntmaps.KittyMedium, loader *neighborhood.Loader, renderer chart.RenderMode) *model {
	places, err := curated.Embedded()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load places: %v\n", err)
		os.Exit(1)
	}
	if len(places) == 0 {
		fmt.Fprintln(os.Stderr, "no places to display")
		os.Exit(1)
	}

	places = append([]curated.Place{{Name: "Flatiron district", Description: "Bundled neighborhood: real building footprints and heights around Flatiron, Manhattan. Press v for 3D.", Lat: 40.7415, Lon: -73.989, Zoom: 17}}, places...)
	items := make([]list.Item, len(places))
	for i, p := range places {
		items[i] = placeItem{p: p}
	}
	zones := zone.New()
	prefix := zones.NewPrefix() + "place-"
	l := list.New(items, placeDelegate{list.NewDefaultDelegate(), zones, prefix}, 0, 0)
	l.Title = "Places"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.KeyMap = listKeyMap()

	first := places[0]
	// Oversample: 2 gives a 2× pixel-density tile-canvas at +1 OSM zoom,
	// which Kitty terminals downscale to a noticeably sharper image at no
	// extra geographic coverage. Glyph mode is unaffected visually.
	cfg := ntmaps.Config{Oversample: 2, KittyMedium: medium, PreferKitty: true}
	if runtime.GOOS != "js" {
		// Animate arrow pans; the prefetch ring keeps the next tiles in memory.
		cfg.SmoothPan = true
	}
	mv := ntmaps.NewWithConfig(cfg)
	mv.KeyMap = mapKeyMap()
	mv.SetLatLng(first.Lat, first.Lon, first.Zoom)
	mv.SetMarkers([]ntmaps.Marker{{Lat: first.Lat, Lng: first.Lon}})

	// The bundled Flatiron archive serves its tiles offline, in front of
	// OpenStreetMap for everywhere else.
	archive, err := pmtiles.New(bytes.NewReader(flatironArchive), pmtiles.Options{Name: "flatiron"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bundled tiles: %v\n", err)
		os.Exit(1)
	}
	m := &model{
		zones: zones, itemPrefix: prefix,
		loader: loader, medium: medium, renderer: renderer, mapMode: ntmaps.RenderGlyph,
		mv:        mv,
		archive:   archive,
		routes:    routes(),
		search:    newSearchInput(),
		places:    places,
		list:      l,
		help:      help.New(),
		keys:      newAppKeys(),
		lastPlace: first,
	}
	m.mv.SetTileSource(m.baseSource())
	m.mv.SetPaths(m.pathsFor(first))
	return m
}

func newSearchInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "Go to: "
	ti.Placeholder = "place name or address"
	ti.CharLimit = 80
	return ti
}

// baseSource is the current style, with the bundled Flatiron tiles in
// front of OpenStreetMap when that style is selected.
func (m *model) baseSource() tile.Source {
	src := styles[m.style].src()
	if m.style == 0 && m.archive != nil {
		return tile.FirstOf(m.archive, src)
	}
	return src
}

func (m *model) pathsFor(p curated.Place) []ntmaps.Path {
	pts, ok := m.routes[p.Name]
	if !ok {
		return nil
	}
	return []ntmaps.Path{{Points: pts, Width: 3}}
}

// syncMapToSelection re-centers the map on the currently-highlighted place,
// returning a render Cmd if the selection actually changed.
func (m *model) syncMapToSelection() tea.Cmd {
	it, ok := m.list.SelectedItem().(placeItem)
	if !ok || it.p == m.lastPlace {
		return nil
	}
	m.cancelDrag()
	m.lastPlace = it.p
	m.mv.SetLatLng(it.p.Lat, it.p.Lon, it.p.Zoom)
	m.mv.SetMarkers([]ntmaps.Marker{{Lat: it.p.Lat, Lng: it.p.Lon}})
	m.mv.SetPaths(m.pathsFor(it.p))
	if m.in3D {
		return m.requestCity(false)
	}
	return m.mv.Init()
}

func (m *model) Init() tea.Cmd { return m.mv.Init() }

type cityLoaded struct {
	generation uint64
	key        string
	result     neighborhood.Result
	err        error
}

func (m *model) stopLoad() {
	m.request++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.loading = false
}
func (m *model) requestCity(refresh bool) tea.Cmd {
	m.cancelDrag()
	m.stopLoad()
	m.loadErr = nil
	m.loading = true
	lat, lon := m.mv.Center()
	center := city.Coordinate{Lat: lat, Lon: lon}
	key := neighborhood.Key(center, 200)
	cleanup := tea.Cmd(nil)
	if m.cityPane != nil {
		cleanup = m.cityPane.Suspend()
	}
	if !refresh && m.cityPane != nil && m.cityContains(center) {
		m.loading = false
		m.cached = true
		x, y := m.cityPane.Scene.Projection.Project(center)
		target := m.cityPane.Scene.Normalize(city.Vec3{X: x, Y: y})
		camera := m.cityPane.Chart.Camera()
		camera.Target.X = float32(target.X)
		camera.Target.Y = float32(target.Y)
		return tea.Sequence(cleanup, tea.Batch(m.cityPane.Chart.SetCamera(camera), m.cityPane.Resume(m.renderer)))
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	generation := m.request
	return tea.Sequence(cleanup, func() tea.Msg {
		defer cancel()
		result, err := m.loader.Load(ctx, center, 200, refresh)
		return cityLoaded{generation, key, result, err}
	})
}
func (m *model) cityContains(c city.Coordinate) bool {
	if m.cityPane == nil || m.cityKey == "" {
		return false
	}
	s := m.cityPane.Scene
	x, y := s.Projection.Project(c)
	return x >= s.Ground[0].X && x <= s.Ground[2].X && y >= s.Ground[0].Y && y <= s.Ground[2].Y
}
func (m *model) toggleView() tea.Cmd {
	m.cancelDrag()
	m.in3D = !m.in3D
	m.keys.in3D = m.in3D
	if m.in3D {
		m.mapMode = m.mv.RenderMode()
		var hide tea.Cmd
		hide = m.mv.SetRenderMode(ntmaps.RenderGlyph)
		return tea.Sequence(hide, m.requestCity(false))
	}
	ready := !m.loading && m.loadErr == nil
	m.stopLoad()
	var cleanup tea.Cmd
	if m.cityPane != nil {
		if ready && m.cityKey != "" {
			center := m.cityPane.Center()
			m.mv.SetLatLng(center.Lat, center.Lon, m.mv.Zoom())
		}
		if ready {
			m.renderer = m.cityPane.Chart.RequestedRenderMode()
		}
		cleanup = m.cityPane.Suspend()
	}
	return tea.Sequence(cleanup, tea.Batch(m.mv.SetRenderMode(m.mapMode), m.mv.Init()))
}
func (m *model) quit() tea.Cmd {
	m.cancelDrag()
	m.closed = true
	m.stopLoad()
	var cleanup []tea.Cmd
	if m.mv.RenderMode() == ntmaps.RenderKitty {
		cleanup = append(cleanup, m.mv.SetRenderMode(ntmaps.RenderGlyph))
	}
	if m.cityPane != nil {
		cleanup = append(cleanup, m.cityPane.Dispose())
	}
	return tea.Sequence(tea.Batch(cleanup...), tea.Quit)
}
func (m *model) Close() {
	m.cancelDrag()
	if m.zones != nil {
		m.zones.Close()
		m.zones = nil
	}
	m.stopLoad()
	m.mv.Close()
	if m.cityPane != nil {
		m.cityPane.Close()
	}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.closed {
		return m, nil
	}
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.cancelDrag()
		m.width, m.height = v.Width, v.Height
		m.help.SetWidth(m.width)
		L := m.layout()
		m.list.SetSize(L.leftInnerW, L.listInnerH)
		cmds := []tea.Cmd{m.mv.SetSize(L.mapInnerW, L.mapInnerH)}
		if m.cityPane != nil {
			cmds = append(cmds, m.cityPane.SetSize(L.mapInnerW, L.mapInnerH))
		}
		return m, tea.Batch(cmds...)
	case tea.MouseMsg:
		return m, m.mouse(v)
	case cityLoaded:
		if v.generation != m.request || !m.in3D {
			return m, nil
		}
		m.loading = false
		m.cancel = nil
		m.loadErr = v.err
		if v.err != nil {
			return m, nil
		}
		var cmd tea.Cmd
		if m.cityPane == nil {
			m.cityPane, m.loadErr = cityview.New(v.result.Data, v.result.Scene, m.renderer, m.medium)
			if m.loadErr != nil {
				return m, nil
			}
			cmd = m.cityPane.Init()
		} else {
			cmd = tea.Batch(m.cityPane.SetScene(v.result.Data, v.result.Scene), m.cityPane.Resume(m.renderer))
		}
		m.cityKey = v.key
		m.cached = v.result.Cached
		L := m.layout()
		return m, tea.Batch(m.cityPane.SetSize(L.mapInnerW, L.mapInnerH), cmd)
	case tea.KeyPressMsg:
		if v.String() == "ctrl+c" {
			return m, m.quit()
		}
		if m.searching {
			return m, m.updateSearch(v)
		}
		if m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(v)
			return m, tea.Batch(cmd, m.syncMapToSelection())
		}
		switch v.String() {
		case "q":
			return m, m.quit()
		case "v":
			return m, m.toggleView()
		case "?":
			m.help.ShowAll = !m.help.ShowAll
			return m, m.resize()
		case "/", "j", "k", "shift+up", "shift+down", "pgup", "pgdown", "home", "end":
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(v)
			return m, tea.Batch(cmd, m.syncMapToSelection())
		}
		if m.in3D {
			if v.String() == "R" {
				return m, m.requestCity(true)
			}
			if m.loading || m.loadErr != nil || m.cityPane == nil {
				return m, nil
			}
			_, cmd := m.cityPane.Update(v)
			m.renderer = m.cityPane.Chart.RequestedRenderMode()
			return m, cmd
		}
		switch v.String() {
		case "+", "=", "-", "_":
			// The widget clamps zoom to the source's range.
			var cmd tea.Cmd
			m.mv, cmd = m.mv.Update(v)
			return m, cmd
		case "g":
			mode := ntmaps.RenderKitty
			if m.mv.RenderMode() == mode {
				mode = ntmaps.RenderGlyph
			}
			return m, m.mv.SetRenderMode(mode)
		case "s":
			m.style = (m.style + 1) % len(styles)
			return m, m.mv.SetTileSource(m.baseSource())
		case "F":
			if m.mv.FitContent() {
				return m, m.mv.Init()
			}
			return m, nil
		case "G":
			m.searching = true
			m.search.SetValue("")
			return m, m.search.Focus()
		case "o":
			if len(m.mv.Overlays()) > 0 {
				return m, m.mv.SetOverlays()
			}
			return m, m.mv.SetOverlays(tile.OpenSeaMap())
		case "f":
			next := ntmaps.FitContain
			switch m.mv.Fit() {
			case ntmaps.FitContain:
				next = ntmaps.FitFill
			case ntmaps.FitFill:
				next = ntmaps.FitCover
			}
			return m, m.mv.SetFit(next)
		case "]":
			return m, m.mv.SetOpticalZoom(m.mv.OpticalZoom() + 1)
		case "[":
			return m, m.mv.SetOpticalZoom(max(0, m.mv.OpticalZoom()-1))
		}
		var cmd tea.Cmd
		m.mv, cmd = m.mv.Update(v)
		return m, cmd
	}
	if coords, ok := msg.(ntmaps.MapCoordinates); ok && coords.Err == nil {
		// A geocoder hit: mark it, drop the place route, and stop the list
		// from re-centering on its selection.
		m.lastPlace = curated.Place{}
		m.mv.SetMarkers([]ntmaps.Marker{{Lat: coords.Lat, Lng: coords.Lng}})
		m.mv.SetPaths(nil)
	}
	// Ownership checks in both components reject unrelated frames. Runtime and
	// late cleanup messages reach both even while one pane is hidden.
	var cmds []tea.Cmd
	var listCmd tea.Cmd
	m.list, listCmd = m.list.Update(msg)
	cmds = append(cmds, listCmd, m.syncMapToSelection())
	var mapCmd tea.Cmd
	m.mv, mapCmd = m.mv.Update(msg)
	cmds = append(cmds, mapCmd)
	if m.cityPane != nil {
		_, cmd := m.cityPane.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}
func (m *model) resize() tea.Cmd {
	return func() tea.Msg { return tea.WindowSizeMsg{Width: m.width, Height: m.height} }
}

// layout describes inner content dimensions (excluding borders) for each box.
type layoutDims struct {
	leftInnerW   int
	listInnerH   int
	detailInnerH int
	mapInnerW    int
	mapInnerH    int
}

// layout sizes the three boxes plus reserves rows for the status footer and
// the help line. Left column inner width grows with the terminal up to
// roughly one third of the screen. Narrow terminals show only the active pane.
func (m *model) layout() layoutDims {
	leftInnerW := min(40, max(24, m.width/3)) - 2

	mapTotalH := m.height - 1 - max(1, lipgloss.Height(m.help.View(m.keys)))
	if mapTotalH < 4 {
		mapTotalH = 4
	}
	detailOuterH := mapTotalH / 3
	if detailOuterH < 4 {
		detailOuterH = 4
	}
	listOuterH := mapTotalH - detailOuterH

	L := layoutDims{
		leftInnerW:   leftInnerW,
		listInnerH:   listOuterH - 2,
		detailInnerH: detailOuterH - 2,
		mapInnerW:    m.width - (leftInnerW + 2) - 2,
		mapInnerH:    mapTotalH - 2,
	}
	if m.width < 75 || m.height < 16 {
		L.mapInnerW = max(1, m.width-2)
	}
	if L.listInnerH < 1 {
		L.listInnerH = 1
	}
	if L.detailInnerH < 1 {
		L.detailInnerH = 1
	}
	if L.mapInnerW < 1 {
		L.mapInnerW = 1
	}
	if L.mapInnerH < 1 {
		L.mapInnerH = 1
	}
	return L
}

func (m *model) detailView(innerW, innerH int) string {
	it, ok := m.list.SelectedItem().(placeItem)
	if !ok {
		return ""
	}
	wrap := lipgloss.NewStyle().Width(innerW)
	parts := []string{
		titleStyle.Render(it.p.Name),
		wrap.Render(it.p.Description),
		dimStyle.Render(fmt.Sprintf("%.4f, %.4f  z%d", it.p.Lat, it.p.Lon, it.p.Zoom)),
	}
	content := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return lipgloss.NewStyle().Width(innerW).Height(innerH).MaxHeight(innerH).Render(content)
}

// updateSearch drives the go-to prompt: Enter geocodes, Esc cancels.
func (m *model) updateSearch(v tea.KeyPressMsg) tea.Cmd {
	switch v.String() {
	case "esc", "ctrl+c":
		m.searching = false
		m.search.Blur()
		return nil
	case "enter":
		m.searching = false
		m.search.Blur()
		query := m.search.Value()
		if query == "" {
			return nil
		}
		m.cancelDrag()
		return m.mv.SetLocation(query, 14)
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(v)
	return cmd
}

func (m *model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		v := tea.NewView("loading…")
		v.AltScreen = true
		v.MouseMode = tea.MouseModeAllMotion
		return v
	}

	L := m.layout()

	listBox := boxStyle.Render(m.list.View())
	detailBox := boxStyle.Render(m.detailView(L.leftInnerW, L.detailInnerH))
	leftCol := lipgloss.JoinVertical(lipgloss.Left, listBox, detailBox)

	pane := m.mv.View().Content
	if m.in3D {
		switch {
		case m.loading:
			pane = "Loading 3D buildings…"
		case m.loadErr != nil:
			pane = "Could not load this neighborhood.\n" + m.loadErr.Error() + "\nR retries · v returns to 2D"
		case m.cityPane != nil:
			pane = m.cityPane.View().Content
		default:
			pane = "Choose a place, then press v for 3D."
		}
	}
	pane = lipgloss.NewStyle().MaxWidth(L.mapInnerW).MaxHeight(L.mapInnerH).Render(pane)
	mapBox := boxStyle.Width(L.mapInnerW + 2).Height(L.mapInnerH + 2).Render(pane)
	body := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, mapBox)
	if m.width < 75 || m.height < 16 {
		body = mapBox
	}

	mode := "Glyph"
	if m.mv.RenderMode() == ntmaps.RenderKitty {
		mode = "Kitty"
	}
	lat, lng := m.mv.Center()
	optical := ""
	if oz := m.mv.OpticalZoom(); oz > 0 {
		optical = fmt.Sprintf("  ×%d", 1<<oz)
	}
	status := fmt.Sprintf("%.4f,%.4f z%d%s  %s  %s  %s",
		lat, lng, m.mv.Zoom(), optical, mode, styles[m.style].name, fitName(m.mv.Fit()))
	if len(m.mv.Overlays()) > 0 {
		status += "  +seamarks"
	}

	if m.in3D {
		status = "3D · " + m.lastPlace.Name + " · 200 m neighborhood"
		if m.cached {
			status += " · cached"
		}
	} else {
		status = "2D · " + status
	}
	if m.searching {
		status = m.search.View() + "   enter: go · esc: cancel"
	}
	footer := footerStyle.Width(m.width).Render(ansi.Truncate(status, m.width, ""))
	helpView := m.help.View(m.keys)
	v := tea.NewView(m.zones.Scan(lipgloss.JoinVertical(lipgloss.Left, body, footer, helpView)))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func main() {
	defaultMedium := "direct"
	if runtime.GOOS == "js" {
		defaultMedium = "shm"
	}
	mediumFlag := flag.String("medium", defaultMedium, "Kitty transport: direct or shm")
	rendererFlag := flag.String("renderer", "gpu", "3D renderer: gpu or software")
	offline := flag.Bool("offline", false, "use cached/bundled neighborhoods only")
	cacheDir := flag.String("cache-dir", defaultCacheDir(), "neighborhood cache directory; empty disables disk cache")
	flag.Parse()
	medium := ntmaps.KittyMediumDirect
	if *mediumFlag == "shm" {
		medium = ntmaps.KittyMediumSharedMemory
	} else if *mediumFlag != "direct" {
		fmt.Fprintln(os.Stderr, "medium must be direct or shm")
		os.Exit(2)
	}
	renderer := chart.WebGPU
	if *rendererFlag == "software" {
		renderer = chart.Software
	} else if *rendererFlag != "gpu" {
		fmt.Fprintln(os.Stderr, "renderer must be gpu or software")
		os.Exit(2)
	}
	loader, err := newLoader(*cacheDir, *offline)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	m := initialModel(medium, loader, renderer)
	err = demorun.Run(m, "explorer", "Explore maps and 3D buildings", demorun.Keys{
		Primary: []key.Binding{demorun.Binding("v", "2D / 3D"), demorun.Binding("j", "Next place"), demorun.Binding("k", "Previous place"), demorun.Binding("left", "Left"), demorun.Binding("right", "Right")},
		More:    []key.Binding{demorun.Binding("+", "Zoom in"), demorun.Binding("-", "Zoom out"), demorun.Binding("t", "Renderer"), demorun.Binding("w", "Move forward"), demorun.Binding("R", "Refresh 3D")},
	})
	m.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func defaultCacheDir() string {
	if runtime.GOOS == "js" {
		return ""
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ntmaps", "neighborhoods-v1")
}
func newLoader(dir string, offline bool) (*neighborhood.Loader, error) {
	l := &neighborhood.Loader{Dir: dir}
	if !offline {
		l.Fetch = (city.Client{}).Fetch
	}
	d, err := cityview.Bundled()
	if err != nil {
		return nil, err
	}
	if err = l.Seed(city.Coordinate{Lat: 40.7415, Lon: -73.989}, 200, d); err != nil {
		return nil, err
	}
	return l, nil
}
