package storage

import (
	"bytes"
	"encoding/binary"
)

// The two pieces of a JPEG's metadata that survive re-encoding. Everything
// else (GPS, camera, timestamps) is dropped on purpose.

const (
	markerSOI  = 0xD8
	markerEOI  = 0xD9
	markerSOS  = 0xDA
	markerAPP1 = 0xE1
	markerAPP2 = 0xE2

	exifOrientationTag = 0x0112
)

var (
	exifHeader = []byte("Exif\x00\x00")
	iccHeader  = []byte("ICC_PROFILE\x00")
)

// jpegMeta is what processing needs from the original file.
type jpegMeta struct {
	// orientation is the EXIF value, 1 to 8; 1 means upright.
	orientation int
	// iccSegments are the APP2 segments carrying the colour profile,
	// marker and length included, in file order. Phones shoot in wide
	// gamuts (Display P3): without the profile, browsers read the colours
	// as sRGB and the photo looks washed out.
	iccSegments [][]byte
}

// readJPEGMeta walks the segments before the image data. Anything
// malformed ends the walk with what was found so far: metadata is a nicety,
// and the decoder decides whether the file is usable.
func readJPEGMeta(data []byte) jpegMeta {
	meta := jpegMeta{orientation: 1}
	if len(data) < 4 || data[0] != 0xFF || data[1] != markerSOI {
		return meta
	}

	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return meta
		}
		marker := data[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		if marker == markerSOS || marker == markerEOI {
			return meta
		}
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		end := i + 2 + length
		if length < 2 || end > len(data) {
			return meta
		}
		payload := data[i+4 : end]

		switch {
		case marker == markerAPP1 && bytes.HasPrefix(payload, exifHeader):
			if o := exifOrientation(payload[len(exifHeader):]); o != 0 {
				meta.orientation = o
			}
		case marker == markerAPP2 && bytes.HasPrefix(payload, iccHeader):
			meta.iccSegments = append(meta.iccSegments, data[i:end])
		}
		i = end
	}
	return meta
}

// exifOrientation reads the orientation tag from IFD0 of a TIFF block, and
// returns 0 when it is absent or unreadable.
func exifOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 0
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 0
	}

	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return 0
	}
	count := int(order.Uint16(tiff[ifd : ifd+2]))
	for n := range count {
		entry := ifd + 2 + n*12
		if entry+12 > len(tiff) {
			return 0
		}
		if order.Uint16(tiff[entry:entry+2]) != exifOrientationTag {
			continue
		}
		// A SHORT, stored in the first two bytes of the value field.
		o := int(order.Uint16(tiff[entry+8 : entry+10]))
		if o < 1 || o > 8 {
			return 0
		}
		return o
	}
	return 0
}

// withICC puts the colour profile segments right after the SOI of an
// encoded JPEG, which is where readers look for them.
func withICC(encoded []byte, segments [][]byte) []byte {
	if len(segments) == 0 || len(encoded) < 2 {
		return encoded
	}
	size := len(encoded)
	for _, s := range segments {
		size += len(s)
	}
	out := make([]byte, 0, size)
	out = append(out, encoded[:2]...)
	for _, s := range segments {
		out = append(out, s...)
	}
	return append(out, encoded[2:]...)
}
