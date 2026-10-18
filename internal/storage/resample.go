package storage

import (
	"image"
	"image/color"
)

// resample shrinks src to w×h by area averaging: each output pixel is the
// mean of the source area it covers. For reducing photos it is as good as
// the usual kernels, and it streams through the source one row at a time.
// x/image/draw's kernel scalers instead hold an intermediate of
// output width × source height × 32 bytes, about 116 MB for a 12 MP phone
// photo, which a 256 MB machine cannot spare.
//
// Transparency is flattened onto white along the way. w and h must not be
// larger than the source: photos are never enlarged.
func resample(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	readRow := rowReader(src)

	cols := areaWeights(sw, w)
	rows := areaWeights(sh, h)

	srcRow := make([]uint8, sw*3)
	hRow := make([]float32, w*3) // the source row, already shrunk across
	acc := make([]float32, w*3)
	cachedY := -1

	for dy := range h {
		clear(acc)
		for _, wy := range rows[dy] {
			if wy.index != cachedY {
				readRow(b.Min.Y+wy.index, srcRow)
				shrinkRow(srcRow, cols, hRow)
				cachedY = wy.index
			}
			for i, v := range hRow {
				acc[i] += v * wy.weight
			}
		}

		out := dst.Pix[dy*dst.Stride:]
		for dx := range w {
			out[dx*4+0] = toByte(acc[dx*3+0])
			out[dx*4+1] = toByte(acc[dx*3+1])
			out[dx*4+2] = toByte(acc[dx*3+2])
			out[dx*4+3] = 0xFF
		}
	}
	return dst
}

type areaWeight struct {
	index  int
	weight float32
}

// areaWeights lists, for each of the n output positions, the source
// positions it covers and how much of each, as fractions summing to 1.
func areaWeights(size, n int) [][]areaWeight {
	scale := float64(size) / float64(n)
	weights := make([][]areaWeight, n)
	for i := range n {
		start, end := float64(i)*scale, float64(i+1)*scale
		for s := int(start); s < size && float64(s) < end; s++ {
			overlap := min(end, float64(s+1)) - max(start, float64(s))
			if overlap <= 0 {
				continue
			}
			weights[i] = append(weights[i], areaWeight{index: s, weight: float32(overlap / scale)})
		}
	}
	return weights
}

func shrinkRow(src []uint8, cols [][]areaWeight, dst []float32) {
	for dx, ws := range cols {
		var r, g, b float32
		for _, wx := range ws {
			p := src[wx.index*3:]
			r += float32(p[0]) * wx.weight
			g += float32(p[1]) * wx.weight
			b += float32(p[2]) * wx.weight
		}
		dst[dx*3+0], dst[dx*3+1], dst[dx*3+2] = r, g, b
	}
}

func toByte(v float32) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v + 0.5)
}

// rowReader returns a function that writes row y of img into dst as RGB,
// three bytes a pixel, flattened onto white. The common decoder outputs
// get direct paths; anything else goes through At, which is slower.
func rowReader(img image.Image) func(y int, dst []uint8) {
	b := img.Bounds()
	switch m := img.(type) {
	case *image.YCbCr:
		return func(y int, dst []uint8) {
			for x := b.Min.X; x < b.Max.X; x++ {
				yi, ci := m.YOffset(x, y), m.COffset(x, y)
				r, g, bl := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
				i := (x - b.Min.X) * 3
				dst[i], dst[i+1], dst[i+2] = r, g, bl
			}
		}
	case *image.RGBA: // premultiplied
		return func(y int, dst []uint8) {
			row := m.Pix[m.PixOffset(b.Min.X, y):]
			for i := range b.Dx() {
				p := row[i*4:]
				white := 255 - p[3]
				dst[i*3], dst[i*3+1], dst[i*3+2] = p[0]+white, p[1]+white, p[2]+white
			}
		}
	case *image.NRGBA: // straight alpha
		return func(y int, dst []uint8) {
			row := m.Pix[m.PixOffset(b.Min.X, y):]
			for i := range b.Dx() {
				p := row[i*4:]
				a := uint32(p[3])
				white := 255 - a
				dst[i*3+0] = uint8((uint32(p[0])*a+127)/255 + white)
				dst[i*3+1] = uint8((uint32(p[1])*a+127)/255 + white)
				dst[i*3+2] = uint8((uint32(p[2])*a+127)/255 + white)
			}
		}
	case *image.Gray:
		return func(y int, dst []uint8) {
			row := m.Pix[m.PixOffset(b.Min.X, y):]
			for i := range b.Dx() {
				dst[i*3], dst[i*3+1], dst[i*3+2] = row[i], row[i], row[i]
			}
		}
	default:
		return func(y int, dst []uint8) {
			for x := b.Min.X; x < b.Max.X; x++ {
				// Premultiplied, 16 bits a channel.
				r, g, bl, a := img.At(x, y).RGBA()
				white := 0xFFFF - a
				i := (x - b.Min.X) * 3
				dst[i], dst[i+1], dst[i+2] = uint8((r+white)>>8), uint8((g+white)>>8), uint8((bl+white)>>8)
			}
		}
	}
}
