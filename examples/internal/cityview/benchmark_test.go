package cityview

import (
	"fmt"
	"io"
	"reflect"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	"nimbleterminal.dev/ntmaps/internal/city"
	picture "nimbleterminal.dev/ntpicture"
)

// BenchmarkCityNavigation includes rendering, Kitty encoding and View generation.
// Protocol output goes to an in-memory sink; terminal decoding/display is excluded.
func BenchmarkCityNavigation(b *testing.B) {
	for _, mode := range []chart.RenderMode{chart.WebGPU, chart.Software} {
		for _, work := range []string{"drag", "look", "move"} {
			b.Run(fmt.Sprintf("%s/%s", mode, work), func(b *testing.B) {
				d, err := Bundled()
				if err != nil {
					b.Fatal(err)
				}
				scene, err := city.Build(d, city.DefaultHeightPolicy())
				if err != nil {
					b.Fatal(err)
				}
				session := picture.NewTerminalSession(picture.TerminalOptions{
					Kitty:            picture.KittyCapabilitySupported,
					SharedMemory:     picture.KittyCapabilityUnsupported,
					FrameEdits:       picture.KittyCapabilityUnsupported,
					FrameAppend:      picture.KittyCapabilityUnsupported,
					FrameSelect:      picture.KittyCapabilityUnsupported,
					VirtualPlacement: picture.KittyCapabilityUnsupported,
					CellPixelWidth:   10, CellPixelHeight: 20,
				})
				defer session.Close()
				m, err := New(d, scene, mode, picture.KittyMediumDirect, session)
				if err != nil {
					b.Fatal(err)
				}
				defer m.Close()
				p := cityPump{b: b, m: m, events: make(chan cityEvent, 256)}
				p.run(m.SetSize(100, 36))
				p.run(m.Init())
				if m.Chart.RenderMode() != mode || m.Chart.Err() != nil {
					b.Fatalf("renderer fallback: requested=%v actual=%v err=%v", mode, m.Chart.RenderMode(), m.Chart.Err())
				}
				m.View()
				time.Sleep(20 * time.Millisecond) // allow Bubblezone to scan the view
				_, cmd := m.Update(tea.MouseClickMsg{X: 40, Y: 12, Button: tea.MouseLeft})
				p.run(cmd)
				p.bytes = 0
				durations := make([]time.Duration, 0, b.N)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					before := m.Chart.Camera()
					var msg tea.Msg
					switch work {
					case "drag":
						x := i % 40
						if x > 20 {
							x = 40 - x
						}
						msg = tea.MouseMotionMsg{X: 41 + x, Y: 12, Button: tea.MouseLeft}
					case "look":
						code := tea.KeyLeft
						if i%24 >= 12 {
							code = tea.KeyRight
						}
						msg = tea.KeyPressMsg{Code: code}
					case "move":
						k := "w"
						if i%24 >= 12 {
							k = "s"
						}
						msg = tea.KeyPressMsg{Code: rune(k[0]), Text: k}
					}
					start := time.Now()
					_, cmd = m.Update(msg)
					p.run(cmd)
					if m.Chart.Camera() == before {
						b.Fatal("navigation did not change camera")
					}
					m.View()
					durations = append(durations, time.Since(start))
					if m.Chart.RenderMode() != mode || m.Chart.Err() != nil {
						b.Fatalf("renderer changed: %v %v", m.Chart.RenderMode(), m.Chart.Err())
					}
				}
				b.StopTimer()
				slices.Sort(durations)
				b.ReportMetric(float64(durations[len(durations)/2].Microseconds())/1000, "p50-ms/frame")
				b.ReportMetric(float64(durations[(len(durations)-1)*95/100].Microseconds())/1000, "p95-ms/frame")
				b.ReportMetric(float64(p.bytes)/float64(b.N), "protocol-B/frame")
				b.Logf("buildings=%d status=%+v", len(scene.Buildings), m.Chart.PictureStatus())
				p.run(m.Dispose())
			})
		}
	}
}

type cityEvent struct {
	msg    tea.Msg
	follow []tea.Cmd
}
type cityPump struct {
	b             *testing.B
	m             *Model
	events        chan cityEvent
	active, bytes int
}

func (p *cityPump) launch(cmd tea.Cmd, follow []tea.Cmd) {
	if cmd == nil {
		if len(follow) > 0 {
			p.launch(follow[0], follow[1:])
		}
		return
	}
	p.active++
	go func() { p.events <- cityEvent{cmd(), follow} }()
}
func (p *cityPump) run(cmd tea.Cmd) {
	before := p.m.Chart.PictureStatus().Presented
	p.launch(cmd, nil)
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for p.active > 0 {
		status := p.m.Chart.PictureStatus()
		if status.Presented > before && !status.Busy {
			return
		}
		select {
		case e := <-p.events:
			p.active--
			switch v := e.msg.(type) {
			case tea.BatchMsg:
				for _, child := range v {
					p.launch(child, nil)
				}
			case tea.RawMsg:
				p.bytes += len(fmt.Sprint(v.Msg))
			default:
				// Sequence messages are private to Bubble Tea, but contain []tea.Cmd.
				value := reflect.ValueOf(e.msg)
				if value.IsValid() && value.Kind() == reflect.Slice && value.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
					next := make([]tea.Cmd, value.Len())
					for i := range next {
						next[i] = value.Index(i).Interface().(tea.Cmd)
					}
					next = append(next, e.follow...)
					if len(next) > 0 {
						p.launch(next[0], next[1:])
					}
					continue
				}
				if err, ok := e.msg.(picture.ErrorMsg); ok {
					p.b.Fatal(err.Err)
				}
				if e.msg != nil {
					_, next := p.m.Update(e.msg)
					p.launch(next, nil)
				}
			}
			if len(e.follow) > 0 {
				p.launch(e.follow[0], e.follow[1:])
			}
		case <-timeout.C:
			p.b.Fatal("city presentation did not settle")
		}
	}
}

// BenchmarkCityNavigationStream drives the real Tea event loop at mouse-report
// rates. Unlike the settled-frame benchmark, inputs continue during rendering.
func BenchmarkCityNavigationStream(b *testing.B) {
	for _, mode := range []chart.RenderMode{chart.Software, chart.WebGPU} {
		b.Run(mode.String(), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				d, err := Bundled()
				if err != nil {
					b.Fatal(err)
				}
				scene, err := city.Build(d, city.DefaultHeightPolicy())
				if err != nil {
					b.Fatal(err)
				}
				session := picture.NewTerminalSession(picture.TerminalOptions{Kitty: picture.KittyCapabilitySupported, SharedMemory: picture.KittyCapabilityUnsupported, FrameEdits: picture.KittyCapabilityUnsupported, FrameAppend: picture.KittyCapabilityUnsupported, FrameSelect: picture.KittyCapabilityUnsupported, VirtualPlacement: picture.KittyCapabilityUnsupported, CellPixelWidth: 10, CellPixelHeight: 20})
				m, err := New(d, scene, mode, picture.KittyMediumDirect, session)
				if err != nil {
					b.Fatal(err)
				}
				measured := &cityStream{Model: m, ready: make(chan struct{})}
				p := tea.NewProgram(measured, tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithWindowSize(100, 36), tea.WithoutSignalHandler())
				go func() {
					select {
					case <-measured.ready:
					case <-time.After(10 * time.Second):
						p.Quit()
						return
					}
					// The first view starts Bubblezone's asynchronous scan.
					time.Sleep(20 * time.Millisecond)
					p.Send(tea.MouseClickMsg{X: 40, Y: 12, Button: tea.MouseLeft})
					ticker := time.NewTicker(time.Second / 120)
					defer ticker.Stop()
					for n := 0; n < 240; n++ {
						<-ticker.C
						x := n % 40
						if x > 20 {
							x = 40 - x
						}
						p.Send(tea.MouseMotionMsg{X: 41 + x, Y: 12, Button: tea.MouseLeft})
					}
					time.Sleep(300 * time.Millisecond)
					p.Send(tea.KeyPressMsg{Code: 'q', Text: "q"})
				}()
				_, err = p.Run()
				_ = m.Close()
				session.Close()
				if err != nil {
					b.Fatal(err)
				}
				b.Logf("requested=%s actual=%s error=%v inputs=%d render-results=%d presented-during-drag=%d max-gap=%s", mode, m.Chart.RenderMode(), m.Chart.Err(), measured.inputs, measured.renders, len(measured.presented), measured.maxGap)
				if measured.inputs != 240 {
					b.Fatalf("incomplete drag: %d input reports", measured.inputs)
				}
				if m.Chart.RenderMode() != mode || m.Chart.Err() != nil {
					b.Fatalf("invalid sample: renderer=%s err=%v", m.Chart.RenderMode(), m.Chart.Err())
				}
				b.ReportMetric(float64(len(measured.presented)), "presented/drag")
				b.ReportMetric(float64(measured.maxGap.Microseconds())/1000, "max-gap-ms")
			}
		})
	}
}

type cityStream struct {
	*Model
	ready           chan struct{}
	readyClosed     bool
	inputs, renders int
	last            time.Time
	presented       []time.Time
	maxGap          time.Duration
}

func (m *cityStream) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	now := time.Now()
	if _, ok := msg.(tea.MouseMotionMsg); ok {
		if m.inputs == 0 {
			m.last = now
		}
		m.inputs++
	}
	if fmt.Sprintf("%T", msg) == "ntcharts3d.rendered" && m.inputs > 0 {
		m.renders++
	}
	if _, ok := msg.(picture.PresentedMsg); ok && m.inputs > 0 && m.inputs < 240 {
		m.maxGap = max(m.maxGap, now.Sub(m.last))
		m.last = now
		m.presented = append(m.presented, now)
	}
	if _, ok := msg.(tea.MouseMotionMsg); ok && m.inputs == 240 {
		m.maxGap = max(m.maxGap, now.Sub(m.last))
	}
	_, cmd := m.Model.Update(msg)
	return m, cmd
}

// Wait until the first frame is presented before measuring mouse movement;
// cold GPU pipeline initialization belongs to startup, not drag latency.
func (m *cityStream) View() tea.View {
	v := m.Model.View()
	status := m.Chart.PictureStatus()
	if !m.readyClosed && status.Presented > 0 && !status.Busy {
		m.readyClosed = true
		close(m.ready)
	}
	return v
}
