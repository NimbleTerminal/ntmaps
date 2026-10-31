package cityview

import (
	"math"

	tea "charm.land/bubbletea/v2"
	chart "github.com/NimbleMarkets/ntcharts3d"
	m3 "nimbleterminal.dev/nt3d/math"
)

// eyeOffset is the vector from the orbit target to the camera eye.
func eyeOffset(c chart.Camera) m3.Vec3 {
	a, b := c.Alpha*math.Pi/180, c.Beta*math.Pi/180
	return m3.Vec3{
		X: float32(c.Distance * math.Cos(a) * math.Cos(b)),
		Y: float32(c.Distance * math.Cos(a) * math.Sin(b)),
		Z: float32(c.Distance * math.Sin(a)),
	}
}

func (m *Model) look(key string) tea.Cmd {
	c := m.Chart.Camera()
	eye := c.Target.Add(eyeOffset(c))
	switch key {
	case "left", "h":
		c.Beta += 5
	case "right", "l":
		c.Beta -= 5
	case "up", "k":
		c.Alpha -= 5
	case "down", "j":
		c.Alpha += 5
	}
	// Clamp before deriving the target so the chart's clamp cannot move the eye.
	c.Alpha = math.Max(-89, math.Min(89, c.Alpha))
	c.Beta = math.Mod(c.Beta, 360)
	c.Target = eye.Sub(eyeOffset(c))
	return m.Chart.SetCamera(c)
}
