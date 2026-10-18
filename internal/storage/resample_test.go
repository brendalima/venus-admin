package storage

import (
	"image"
	"image/color"
	"math"
	"testing"
)

func TestAreaWeightsSumToOne(t *testing.T) {
	for _, c := range []struct{ size, n int }{{4032, 1200}, {3024, 1800}, {1200, 600}, {7, 3}, {5, 5}} {
		for i, ws := range areaWeights(c.size, c.n) {
			var sum float64
			for _, w := range ws {
				sum += float64(w.weight)
			}
			if math.Abs(sum-1) > 1e-4 {
				t.Errorf("%d→%d: output %d weighs %f, want 1", c.size, c.n, i, sum)
			}
		}
	}
}

func TestResample(t *testing.T) {
	// A black and white checkerboard averages to mid grey.
	board := image.NewGray(image.Rect(0, 0, 400, 300))
	for y := range 300 {
		for x := range 400 {
			if (x+y)%2 == 0 {
				board.SetGray(x, y, color.Gray{Y: 255})
			}
		}
	}
	// Each output pixel covers 2×2 squares, half of them white. (With odd
	// blocks the count is uneven, so this check needs an even factor.)
	out := resample(board, 200, 150)
	for _, p := range []image.Point{{0, 0}, {100, 75}, {199, 149}} {
		c := out.RGBAAt(p.X, p.Y)
		if c.R < 120 || c.R > 135 || c.R != c.G || c.G != c.B || c.A != 255 {
			t.Errorf("pixel %v = %v, want mid grey", p, c)
		}
	}

	// A flat colour stays exactly that colour, through every row reader.
	sources := map[string]image.Image{
		"rgba":  image.NewRGBA(image.Rect(0, 0, 90, 60)),
		"nrgba": image.NewNRGBA(image.Rect(0, 0, 90, 60)),
		"ycbcr": image.NewYCbCr(image.Rect(0, 0, 90, 60), image.YCbCrSubsampleRatio420),
		"other": image.NewRGBA64(image.Rect(0, 0, 90, 60)),
	}
	want := color.RGBA{R: 200, G: 100, B: 50, A: 255}
	for name, src := range sources {
		fill(src, want)
		got := resample(src, 31, 20).RGBAAt(15, 10)
		if diff(got.R, want.R) > 2 || diff(got.G, want.G) > 2 || diff(got.B, want.B) > 2 {
			t.Errorf("%s: flat colour came out as %v, want %v", name, got, want)
		}
	}
}

func fill(img image.Image, c color.RGBA) {
	b := img.Bounds()
	switch m := img.(type) {
	case *image.YCbCr:
		y, cb, cr := color.RGBToYCbCr(c.R, c.G, c.B)
		for i := range m.Y {
			m.Y[i] = y
		}
		for i := range m.Cb {
			m.Cb[i], m.Cr[i] = cb, cr
		}
	case interface{ Set(x, y int, c color.Color) }:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				m.Set(x, y, c)
			}
		}
	}
}

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
