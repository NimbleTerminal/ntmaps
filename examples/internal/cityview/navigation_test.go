package cityview

import (
	"math"
	"testing"

	tea "charm.land/bubbletea/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	m3 "nimbleterminal.dev/nt3d/math"
	picture "nimbleterminal.dev/ntpicture"
)

func TestLookDirectionKeepsEyeFixed(t *testing.T) {
	d, s := fixture(t)
	m, err := New(d, s, chart.Wireframe, picture.KittyMediumDirect)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for _, heading := range []float64{-90, 0, 90, 180} {
		for _, key := range []string{"left", "right", "up", "down", "h", "l", "k", "j"} {
			c := chart.DefaultCamera()
			c.Alpha = 0
			c.Beta = heading
			c.Distance = 3
			c.Target = m3.Vec3{}
			m.Chart.SetCamera(c)
			m.Update(tea.KeyPressMsg{Text: key})
			got := m.Chart.Camera()
			b := heading * math.Pi / 180
			eye := m3.Vec3{X: float32(3 * math.Cos(b)), Y: float32(3 * math.Sin(b))}
			a2, b2 := got.Alpha*math.Pi/180, got.Beta*math.Pi/180
			actual := got.Target.Add(m3.Vec3{X: float32(3 * math.Cos(a2) * math.Cos(b2)), Y: float32(3 * math.Cos(a2) * math.Sin(b2)), Z: float32(3 * math.Sin(a2))})
			if distanceSquared(actual, eye) > 1e-10 {
				t.Fatalf("%s at %g moved eye: %v -> %v", key, heading, eye, actual)
			}
			delta := got.Target.Sub(c.Target)
			right := m3.Vec3{X: float32(-math.Sin(b)), Y: float32(math.Cos(b))}
			switch key {
			case "right", "l":
				if delta.Dot(right) <= 0 {
					t.Fatal("right looked left")
				}
			case "left", "h":
				if delta.Dot(right) >= 0 {
					t.Fatal("left looked right")
				}
			case "up", "k":
				if delta.Z <= 0 {
					t.Fatal("up looked down")
				}
			case "down", "j":
				if delta.Z >= 0 {
					t.Fatal("down looked up")
				}
			}
			if got.Distance != c.Distance {
				t.Fatal("look changed zoom")
			}
		}
	}
	for _, alpha := range []float64{-88, 88} {
		c := chart.DefaultCamera()
		c.Alpha = alpha
		m.Chart.SetCamera(c)
		eye := c.Target.Add(eyeOffset(c))
		key := "up"
		if alpha > 0 {
			key = "down"
		}
		for range 3 {
			m.Update(tea.KeyPressMsg{Text: key})
		}
		got := m.Chart.Camera()
		if math.Abs(got.Alpha) != 89 || distanceSquared(got.Target.Add(eyeOffset(got)), eye) > 1e-10 {
			t.Fatal("elevation clamp moved eye")
		}
	}
}

func TestCityHorizontalWheelIgnored(t *testing.T) {
	d, s := fixture(t)
	m, err := New(d, s, chart.Wireframe, picture.KittyMediumDirect)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.View()
	before := m.Chart.Camera()
	for _, button := range []tea.MouseButton{tea.MouseWheelLeft, tea.MouseWheelRight} {
		m.Update(tea.MouseWheelMsg{X: 20, Y: 10, Button: button})
		if m.Chart.Camera() != before {
			t.Fatal("horizontal wheel changed camera")
		}
	}
}

func distanceSquared(a, b m3.Vec3) float32 { d := a.Sub(b); return d.Dot(d) }
