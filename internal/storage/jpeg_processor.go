package storage

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"

	"golang.org/x/image/webp"

	"github.com/brendalima/venus-admin/internal/core/domain"
)

var _ ImageProcessor = (*JPEGProcessor)(nil)

const (
	// The widths the site serves; see domain.Image.
	largeWidth = 1200
	smallWidth = 600

	jpegQuality = 82

	// maxDecodedBytes caps the memory a photo may take once decoded. The
	// machine has 256 MB and the app needs some of it; a 12 MP phone JPEG
	// takes about 18 MB, a 48 MP one about 72 MB.
	maxDecodedBytes = 128 << 20
)

// JPEGProcessor resizes photos in pure Go and writes JPEG. It needs no C
// library, so the binary stays static; libvips with WebP would make files
// about a quarter smaller, and can replace this behind ImageProcessor.
//
// Re-encoding drops all metadata, EXIF included, which on product photos
// routinely carries the GPS coordinates of the shoot. The two parts that
// matter are applied first: the orientation (phones store photos sideways
// and say so in EXIF) and the colour profile.
type JPEGProcessor struct {
	// One photo at a time: decoding is where the memory goes.
	slot chan struct{}
}

func NewJPEGProcessor() *JPEGProcessor {
	return &JPEGProcessor{slot: make(chan struct{}, 1)}
}

func (p *JPEGProcessor) Process(ctx context.Context, original []byte) (small, large []byte, err error) {
	select {
	case p.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	defer func() { <-p.slot }()

	src, meta, err := decode(original)
	if err != nil {
		return nil, nil, err
	}

	largeImg := resize(src, meta.orientation, largeWidth)
	smallImg := resize(largeImg, 1, smallWidth)

	if large, err = encode(largeImg, meta.iccSegments); err != nil {
		return nil, nil, err
	}
	if small, err = encode(smallImg, meta.iccSegments); err != nil {
		return nil, nil, err
	}
	return small, large, nil
}

// decode checks what the file really is, by its first bytes rather than
// its name or the type the browser sent, and that it fits in memory
// before decoding it.
func decode(data []byte) (image.Image, jpegMeta, error) {
	meta := jpegMeta{orientation: 1}
	var decodeConfig func([]byte) (image.Config, error)
	var decodeImage func([]byte) (image.Image, error)

	switch http.DetectContentType(data) {
	case "image/jpeg":
		meta = readJPEGMeta(data)
		decodeConfig = func(b []byte) (image.Config, error) { return jpeg.DecodeConfig(bytes.NewReader(b)) }
		decodeImage = func(b []byte) (image.Image, error) { return jpeg.Decode(bytes.NewReader(b)) }
	case "image/png":
		decodeConfig = func(b []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(b)) }
		decodeImage = func(b []byte) (image.Image, error) { return png.Decode(bytes.NewReader(b)) }
	case "image/webp":
		decodeConfig = func(b []byte) (image.Config, error) { return webp.DecodeConfig(bytes.NewReader(b)) }
		decodeImage = func(b []byte) (image.Image, error) { return webp.Decode(bytes.NewReader(b)) }
	default:
		return nil, meta, domain.NewValidationError("photos", "Esse arquivo não é uma foto em JPEG, PNG ou WebP.")
	}

	cfg, err := decodeConfig(data)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, meta, domain.NewValidationError("photos", "Não consegui abrir essa foto. O arquivo pode estar danificado.")
	}
	if int64(cfg.Width)*int64(cfg.Height)*bytesPerPixel(cfg.ColorModel) > maxDecodedBytes {
		return nil, meta, domain.NewValidationError("photos",
			"Essa foto é grande demais para o painel. Use uma foto tirada no modo normal da câmera.")
	}

	img, err := decodeImage(data)
	if err != nil {
		return nil, meta, domain.NewValidationError("photos", "Não consegui abrir essa foto. O arquivo pode estar danificado.")
	}
	return img, meta, nil
}

// bytesPerPixel estimates the decoded size, generously for formats whose
// layout depends on the file.
func bytesPerPixel(m color.Model) int64 {
	switch m {
	case color.YCbCrModel, color.GrayModel:
		return 3 // worst case: 4:4:4 chroma
	case color.RGBA64Model, color.NRGBA64Model, color.Gray16Model:
		return 8
	default:
		return 4
	}
}

// resize scales src so that, once turned upright, it is at most width
// pixels wide, then turns it upright. Scaling first means rotating a small
// image instead of the full one. Photos narrower than width are not
// enlarged. Transparency is flattened onto white, since JPEG has none.
// The decoded source is the only full-size copy that ever exists.
func resize(src image.Image, orientation, width int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	swap := orientation >= 5

	uprightW, uprightH := w, h
	if swap {
		uprightW, uprightH = h, w
	}
	if uprightW > width {
		uprightH = max(1, (uprightH*width+uprightW/2)/uprightW)
		uprightW = width
	}

	scaledW, scaledH := uprightW, uprightH
	if swap {
		scaledW, scaledH = uprightH, uprightW
	}

	dst := resample(src, scaledW, scaledH)
	return orient(dst, orientation)
}

// orient applies an EXIF orientation, returning an upright image.
func orient(src *image.RGBA, orientation int) *image.RGBA {
	if orientation < 2 || orientation > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	for dy := range dh {
		for dx := range dw {
			var sx, sy int
			switch orientation {
			case 2: // mirrored
				sx, sy = w-1-dx, dy
			case 3: // upside down
				sx, sy = w-1-dx, h-1-dy
			case 4: // mirrored upside down
				sx, sy = dx, h-1-dy
			case 5: // transposed
				sx, sy = dy, dx
			case 6: // needs a quarter turn clockwise
				sx, sy = dy, h-1-dx
			case 7: // transversed
				sx, sy = w-1-dy, h-1-dx
			case 8: // needs a quarter turn anticlockwise
				sx, sy = w-1-dy, dx
			}
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

func encode(img image.Image, icc [][]byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("couldn't encode jpeg: %w", err)
	}
	return withICC(buf.Bytes(), icc), nil
}
