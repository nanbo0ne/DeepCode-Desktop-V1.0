package control

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/image/riff"
	"golang.org/x/image/vp8"
	"golang.org/x/image/vp8l"
	_ "golang.org/x/image/webp"
)

// DecodeConfig may return the VP8X canvas before inspecting the image payload.
// Bound both headers before Decode can allocate pixels or an alpha plane, and
// require them to agree. RIFF and frame parsing use the same upstream parsers
// as the actual WebP decoder; no compressed bitstream is decoded here.
func validateTaskWebPHeaders(raw []byte) error {
	if len(raw) < 12 || uint64(binary.LittleEndian.Uint32(raw[4:8]))+8 != uint64(len(raw)) {
		return fmt.Errorf("invalid WebP container length")
	}
	form, chunks, err := riff.NewReader(bytes.NewReader(raw))
	if err != nil || string(form[:]) != "WEBP" {
		return fmt.Errorf("invalid WebP container")
	}
	var canvasWidth, canvasHeight, frameWidth, frameHeight int
	var extended, alpha, frame bool
	var flags byte
	for {
		id, size, data, err := chunks.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid WebP chunk: %w", err)
		}
		switch string(id[:]) {
		case "VP8X":
			if extended || frame || alpha || size != 10 {
				return fmt.Errorf("invalid WebP extended header")
			}
			var header [10]byte
			if _, err := io.ReadFull(data, header[:]); err != nil {
				return fmt.Errorf("invalid WebP extended header: %w", err)
			}
			flags = header[0]
			if flags&0x02 != 0 {
				return fmt.Errorf("task images require non-animated WebP")
			}
			if flags&0xc1 != 0 || header[1] != 0 || header[2] != 0 || header[3] != 0 {
				return fmt.Errorf("invalid WebP reserved header bits")
			}
			canvasWidth = 1 + int(header[4]) + int(header[5])<<8 + int(header[6])<<16
			canvasHeight = 1 + int(header[7]) + int(header[8])<<8 + int(header[9])<<16
			if err := validateTaskImageDimensions(canvasWidth, canvasHeight); err != nil {
				return err
			}
			extended = true
		case "ALPH":
			if !extended || flags&0x10 == 0 || alpha || frame {
				return fmt.Errorf("invalid WebP alpha chunk")
			}
			alpha = true
		case "ANIM", "ANMF":
			return fmt.Errorf("task images require non-animated WebP")
		case "VP8 ", "VP8L":
			if frame {
				return fmt.Errorf("multiple WebP image payloads")
			}
			if string(id[:]) == "VP8 " {
				if extended && (flags&0x10 != 0) != alpha {
					return fmt.Errorf("WebP alpha flag does not match its payload")
				}
				decoder := vp8.NewDecoder()
				decoder.Init(data, int(size))
				header, err := decoder.DecodeFrameHeader()
				if err != nil {
					return fmt.Errorf("invalid WebP VP8 header: %w", err)
				}
				frameWidth, frameHeight = header.Width, header.Height
			} else {
				if alpha {
					return fmt.Errorf("invalid WebP lossless alpha chunk")
				}
				header, err := vp8l.DecodeConfig(data)
				if err != nil {
					return fmt.Errorf("invalid WebP VP8L header: %w", err)
				}
				frameWidth, frameHeight = header.Width, header.Height
			}
			if err := validateTaskImageDimensions(frameWidth, frameHeight); err != nil {
				return err
			}
			if extended && (frameWidth != canvasWidth || frameHeight != canvasHeight) {
				return fmt.Errorf("WebP canvas and image dimensions do not match")
			}
			frame = true
		}
	}
	if !frame {
		return fmt.Errorf("WebP has no image payload")
	}
	return nil
}
