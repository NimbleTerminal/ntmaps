//go:build webgpu

package main

import (
	chart "github.com/NimbleMarkets/ntcharts3d"
	"testing"
)

// GPU benchmarks fail on fallback so CPU samples cannot be mislabeled.
func BenchmarkMap3DGPU(b *testing.B) {
	for _, work := range []string{"camera", "texture"} {
		b.Run(work, func(b *testing.B) {
			c, src := benchmarkChart(b, 4)
			c.SetRenderMode(chart.WebGPU)
			draw := func() {
				img, mode, err := c.Snapshot(640, 320)
				if err != nil || mode != chart.WebGPU || img == nil {
					b.Fatalf("GPU required: mode=%v err=%v", mode, err)
				}
			}
			draw()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if work == "texture" {
					tex, err := chart.NewTexture(src)
					if err != nil {
						b.Fatal(err)
					}
					c.SetSeriesTexture("map", tex)
				}
				camera := initialCamera()
				camera.Beta += float64(i%72) * 5
				c.SetCamera(camera)
				draw()
			}
		})
	}
}
