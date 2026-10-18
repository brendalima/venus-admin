package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var red = color.RGBA{R: 255, A: 255}

// testPhoto is a w×h grey image with a red block in its top-left corner,
// so where the corner ends up shows how the photo was turned.
func testPhoto(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{R: 128, G: 128, B: 128, A: 255}
			if x < w/4 && y < h/4 {
				c = red
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// exifSegment builds an APP1 segment whose IFD0 holds only the
// orientation, in the given byte order.
func exifSegment(orientation int, order binary.ByteOrder) []byte {
	tiff := make([]byte, 8+2+12+4)
	if order == binary.LittleEndian {
		copy(tiff, "II")
	} else {
		copy(tiff, "MM")
	}
	order.PutUint16(tiff[2:], 42)
	order.PutUint32(tiff[4:], 8)
	order.PutUint16(tiff[8:], 1) // one entry
	order.PutUint16(tiff[10:], exifOrientationTag)
	order.PutUint16(tiff[12:], 3) // SHORT
	order.PutUint32(tiff[14:], 1) // one value
	order.PutUint16(tiff[18:], uint16(orientation))

	payload := append(append([]byte{}, exifHeader...), tiff...)
	return segment(markerAPP1, payload)
}

func iccSegment(profile string) []byte {
	payload := append(append([]byte{}, iccHeader...), 1, 1) // chunk 1 of 1
	return segment(markerAPP2, append(payload, profile...))
}

func segment(marker byte, payload []byte) []byte {
	s := []byte{0xFF, marker, 0, 0}
	binary.BigEndian.PutUint16(s[2:], uint16(len(payload)+2))
	return append(s, payload...)
}

// withSegments inserts segments right after the SOI, as a camera would.
func withSegments(jpg []byte, segments ...[]byte) []byte {
	return withICC(jpg, segments)
}

func decodeJPEG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("output is not a JPEG: %v", err)
	}
	return img
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r>>8 > 200 && g>>8 < 80 && b>>8 < 80
}

func TestReadJPEGMeta(t *testing.T) {
	jpg := encodeJPEG(t, testPhoto(8, 8))
	icc := iccSegment("profile bytes")

	tests := []struct {
		name            string
		data            []byte
		wantOrientation int
		wantICC         int
	}{
		{name: "no metadata", data: jpg, wantOrientation: 1},
		{name: "little endian", data: withSegments(jpg, exifSegment(6, binary.LittleEndian)), wantOrientation: 6},
		{name: "big endian", data: withSegments(jpg, exifSegment(8, binary.BigEndian)), wantOrientation: 8},
		{name: "out of range", data: withSegments(jpg, exifSegment(9, binary.BigEndian)), wantOrientation: 1},
		{name: "colour profile", data: withSegments(jpg, exifSegment(3, binary.LittleEndian), icc), wantOrientation: 3, wantICC: 1},
		{name: "truncated", data: withSegments(jpg, exifSegment(6, binary.LittleEndian))[:20], wantOrientation: 1},
		{name: "not a jpeg", data: []byte("hello, world"), wantOrientation: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := readJPEGMeta(tt.data)
			if m.orientation != tt.wantOrientation || len(m.iccSegments) != tt.wantICC {
				t.Errorf("got orientation %d and %d ICC segments, want %d and %d",
					m.orientation, len(m.iccSegments), tt.wantOrientation, tt.wantICC)
			}
		})
	}
}

func TestOrientRestoresUpright(t *testing.T) {
	upright := testPhoto(8, 4)
	// The inverse of each orientation: what a camera stores so that
	// applying the orientation gives the upright photo back.
	inverse := map[int]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 6: 8, 7: 7, 8: 6}

	for o := 1; o <= 8; o++ {
		stored := orient(upright, inverse[o])
		got := orient(stored, o)
		if !bytes.Equal(got.Pix, upright.Pix) || got.Bounds() != upright.Bounds() {
			t.Errorf("orientation %d does not restore the upright photo", o)
		}
	}

	// And one turn checked by hand, so the pair above is not merely
	// consistently wrong: 6 turns clockwise, taking the top-left corner
	// to the top-right.
	turned := orient(upright, 6)
	if turned.Bounds().Dx() != 4 || turned.Bounds().Dy() != 8 {
		t.Fatalf("turned size %v, want 4×8", turned.Bounds())
	}
	if !isRed(turned.At(3, 0)) || isRed(turned.At(0, 0)) {
		t.Error("orientation 6 did not turn the photo clockwise")
	}
}

func TestProcess(t *testing.T) {
	icc := iccSegment("display p3")

	tests := []struct {
		name       string
		data       []byte
		wantLarge  image.Point
		wantSmall  image.Point
		redCorner  image.Point // in the large output, as a fraction: 0 left/top, 1 right/bottom
		wantICC    bool
		wantNoExif bool
	}{
		{
			name:      "landscape",
			data:      encodeJPEG(t, testPhoto(3000, 2000)),
			wantLarge: image.Pt(1200, 800), wantSmall: image.Pt(600, 400),
		},
		{
			name:      "phone photo stored sideways",
			data:      withSegments(encodeJPEG(t, testPhoto(3000, 2000)), exifSegment(6, binary.BigEndian), icc),
			wantLarge: image.Pt(1200, 1800), wantSmall: image.Pt(600, 900),
			redCorner: image.Pt(1, 0), wantICC: true, wantNoExif: true,
		},
		{
			name:      "smaller than both widths",
			data:      encodeJPEG(t, testPhoto(400, 300)),
			wantLarge: image.Pt(400, 300), wantSmall: image.Pt(400, 300),
		},
	}

	p := NewJPEGProcessor()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			small, large, err := p.Process(context.Background(), tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for name, out := range map[string][]byte{"large": large, "small": small} {
				want := tt.wantLarge
				if name == "small" {
					want = tt.wantSmall
				}
				img := decodeJPEG(t, out)
				if got := img.Bounds().Size(); got != want {
					t.Errorf("%s is %v, want %v", name, got, want)
				}
				m := readJPEGMeta(out)
				if tt.wantICC && (len(m.iccSegments) != 1 || !bytes.Equal(m.iccSegments[0], icc)) {
					t.Errorf("%s lost the colour profile", name)
				}
				if tt.wantNoExif && (m.orientation != 1 || bytes.Contains(out, exifHeader)) {
					t.Errorf("%s still carries EXIF", name)
				}
			}

			img := decodeJPEG(t, large)
			b := img.Bounds()
			x := 5 + tt.redCorner.X*(b.Dx()-11)
			y := 5 + tt.redCorner.Y*(b.Dy()-11)
			if !isRed(img.At(x, y)) {
				t.Errorf("the red corner is not at %v of the output", tt.redCorner)
			}
		})
	}
}

func TestProcessFlattensTransparencyOntoWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 50, 50)) // fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	_, large, err := NewJPEGProcessor().Process(context.Background(), buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decodeJPEG(t, large).At(25, 25).RGBA()
	if r>>8 < 245 || g>>8 < 245 || b>>8 < 245 {
		t.Errorf("transparent area came out as (%d, %d, %d), want white", r>>8, g>>8, b>>8)
	}
}

// pngClaiming is a valid PNG header for a w×h image, with no pixel data:
// enough for DecodeConfig, which is all a decompression bomb needs to be
// caught by.
func pngClaiming(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8 bits, RGBA

	chunk := make([]byte, 8, 8+13+4)
	binary.BigEndian.PutUint32(chunk, 13)
	copy(chunk[4:], "IHDR")
	chunk = append(chunk, ihdr...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	return append([]byte("\x89PNG\r\n\x1a\n"), chunk...)
}

func TestProcessRejects(t *testing.T) {
	tests := map[string][]byte{
		"not an image":  []byte("<html>surprise</html>"),
		"empty":         {},
		"gif":           []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"),
		"too many px":   pngClaiming(20000, 20000),
		"broken jpeg":   encodeJPEG(t, testPhoto(100, 100))[:200],
		"header only":   pngClaiming(100, 100),
		"heic from iOS": append([]byte("\x00\x00\x00\x18ftypheic"), make([]byte, 64)...),
	}
	p := NewJPEGProcessor()
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := p.Process(context.Background(), data)
			var v *domain.ValidationError
			if !errors.As(err, &v) || v.Message == "" {
				t.Errorf("err = %v, want a ValidationError with a message", err)
			}
		})
	}
}

func TestProcessWaitsForItsTurn(t *testing.T) {
	p := NewJPEGProcessor()
	p.slot <- struct{}{} // another photo in progress

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := p.Process(ctx, encodeJPEG(t, testPhoto(10, 10))); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled while another photo is processed", err)
	}
}

func BenchmarkProcess12MP(b *testing.B) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testPhoto(4032, 3024), &jpeg.Options{Quality: 90}); err != nil {
		b.Fatal(err)
	}
	p := NewJPEGProcessor()
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := p.Process(context.Background(), buf.Bytes()); err != nil {
			b.Fatal(err)
		}
	}
}
