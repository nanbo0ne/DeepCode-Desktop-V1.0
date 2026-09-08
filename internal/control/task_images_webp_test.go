package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/nanbo0ne/O.R.C.A-for-Windows/internal/provider"
)

// Locally generated 2x2 red fixtures. No external files, encoders or network
// are needed when tests run. Cover both WebP compressed payload formats.
const taskWebPLossy = "UklGRjwAAABXRUJQVlA4IDAAAADQAQCdASoCAAIAAgA0JaACdLoB+AADsAD+8Oj3/yC5YXXI1/8gP+QH/ID/+PIAAAA="
const taskWebPLossless = "UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ9Y/+ByKi/wEA"
const taskWebPLosslessAlpha = "UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAEAcQ/Y/+BSKi/wEA"

func taskWebPBytes(t *testing.T, encoded string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func taskWebPChunk(id string, data []byte) []byte {
	chunk := make([]byte, 8+len(data)+len(data)%2)
	copy(chunk, id)
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(len(data)))
	copy(chunk[8:], data)
	return chunk
}

func taskWebPContainer(chunks ...[]byte) []byte {
	raw := []byte("RIFF\x00\x00\x00\x00WEBP")
	for _, chunk := range chunks {
		raw = append(raw, chunk...)
	}
	binary.LittleEndian.PutUint32(raw[4:8], uint32(len(raw)-8))
	return raw
}

func taskWebPCanvas(width, height int, flags byte) []byte {
	var header [10]byte
	header[0] = flags
	for i := 0; i < 3; i++ {
		header[4+i] = byte((width - 1) >> (8 * i))
		header[7+i] = byte((height - 1) >> (8 * i))
	}
	return taskWebPChunk("VP8X", header[:])
}

func TestTaskImagesCompatibleFormatsIncludingWebP(t *testing.T) {
	lossy, lossless := taskWebPBytes(t, taskWebPLossy), taskWebPBytes(t, taskWebPLossless)
	// ALPH's compressed form uses the VP8L green channel without its header.
	compressedAlpha := append([]byte{1}, lossless[25:20+int(binary.LittleEndian.Uint32(lossless[16:20]))]...)
	im := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	im.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var jpg, gifData bytes.Buffer
	if err := jpeg.Encode(&jpg, im, nil); err != nil {
		t.Fatal(err)
	}
	// GIF's logical canvas may exceed the first frame's bounds; retain support.
	frame := image.NewPaletted(image.Rect(1, 1, 2, 2), color.Palette{color.Black, color.White})
	if err := gif.EncodeAll(&gifData, &gif.GIF{Image: []*image.Paletted{frame}, Delay: []int{0}, Config: image.Config{Width: 4, Height: 4, ColorModel: frame.Palette}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mime string
		raw        []byte
	}{
		{"png", "image/png", taskImagePNG(t, 10)},
		{"jpeg", "image/jpeg", jpg.Bytes()},
		{"gif offset frame", "image/gif", gifData.Bytes()},
		{"webp lossy", "image/webp", lossy},
		{"webp lossless", "image/webp", lossless},
		{"webp lossless alpha", "image/webp", taskWebPBytes(t, taskWebPLosslessAlpha)},
		{"webp extended lossy", "image/webp", taskWebPContainer(taskWebPCanvas(2, 2, 0), lossy[12:])},
		{"webp extended lossless", "image/webp", taskWebPContainer(taskWebPCanvas(2, 2, 0), lossless[12:])},
		{"webp lossy alpha", "image/webp", taskWebPContainer(taskWebPCanvas(2, 2, 0x10), taskWebPChunk("ALPH", []byte{0, 128, 128, 128, 128}), lossy[12:])},
		{"webp lossy compressed alpha", "image/webp", taskWebPContainer(taskWebPCanvas(2, 2, 0x10), taskWebPChunk("ALPH", compressedAlpha), lossy[12:])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mime, err := validateTaskImage(tc.raw)
			if err != nil || mime != tc.mime {
				t.Fatalf("mime=%s err=%v", mime, err)
			}
		})
	}
}

func TestTaskImagesWebPMalformedAndDecodeBudget(t *testing.T) {
	lossy, lossless := taskWebPBytes(t, taskWebPLossy), taskWebPBytes(t, taskWebPLossless)
	hugeVP8 := append([]byte(nil), lossy[20:30]...)
	binary.LittleEndian.PutUint16(hugeVP8[6:8], 16383)
	binary.LittleEndian.PutUint16(hugeVP8[8:10], 16383)
	hugeVP8L := []byte{0x2f, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(hugeVP8L[1:], uint32(16383)|(uint32(16383)<<14))
	for _, tc := range []struct {
		name, want string
		raw        []byte
	}{
		{"RIFF truncation", "container", lossless[:len(lossless)-1]},
		{"missing image", "no image", taskWebPContainer(taskWebPCanvas(2, 2, 0))},
		{"truncated VP8", "invalid", taskWebPContainer(taskWebPChunk("VP8 ", lossy[20:30]))},
		{"truncated VP8L", "invalid", taskWebPContainer(taskWebPChunk("VP8L", lossless[20:25]))},
		{"huge canvas", "dimensions", taskWebPContainer(taskWebPCanvas(65536, 1024, 0), lossless[12:])},
		{"huge VP8 payload behind small canvas", "dimensions", taskWebPContainer(taskWebPCanvas(2, 2, 0), taskWebPChunk("VP8 ", hugeVP8))},
		{"huge VP8L payload behind small canvas", "dimensions", taskWebPContainer(taskWebPCanvas(2, 2, 0), taskWebPChunk("VP8L", hugeVP8L))},
		{"huge alpha allocation", "dimensions", taskWebPContainer(taskWebPCanvas(65536, 1024, 0x10), taskWebPChunk("ALPH", []byte{0}), lossy[12:])},
		{"canvas mismatch", "do not match", taskWebPContainer(taskWebPCanvas(1, 1, 0), lossless[12:])},
		{"duplicate frame", "multiple", taskWebPContainer(lossy[12:], lossless[12:])},
		{"animation", "non-animated", taskWebPContainer(taskWebPCanvas(2, 2, 2), lossy[12:])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateTaskImage(tc.raw); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want=%q", err, tc.want)
			}
		})
	}
}

func TestTaskImagesWebPWorkspaceAndAttachmentAliases(t *testing.T) {
	root := t.TempDir()
	raw := taskWebPBytes(t, taskWebPLossless)
	putTaskImage(t, root, "frame.webp", raw)
	images, _, err := resolveTaskImages(context.Background(), root, []string{"frame.webp"}, "selected/vision", nil, allowTaskImages(), 8, maxVisionImageBytesPerTurn)
	if err != nil || len(images) != 1 {
		t.Fatalf("images=%v err=%v", images, err)
	}
	attached := images[0]
	if attached.MediaType != "image/webp" || !strings.HasSuffix(attached.Path, ".webp") || attached.Data != base64.StdEncoding.EncodeToString(raw) {
		t.Fatalf("unexpected WebP snapshot: %+v", attached)
	}
	putTaskImage(t, root, attached.Path, []byte("replacement must not be read"))
	for _, ref := range []string{attached.Name, attached.Path} {
		images, _, err := resolveTaskImages(context.Background(), root, []string{ref}, "selected/vision", []provider.ImageContent{attached}, allowTaskImages(), 8, maxVisionImageBytesPerTurn)
		if err != nil || len(images) != 1 || images[0].Data != attached.Data {
			t.Fatalf("ref=%s images=%v err=%v", ref, images, err)
		}
	}
}
