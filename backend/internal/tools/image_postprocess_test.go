package tools

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// makeChromaSprite builds a WxH magenta (#ff00ff) image with an opaque red
// square of side `inner` centered in it, encoded as PNG bytes.
func makeChromaSprite(w, h, inner int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = 255
			img.Pix[i+1] = 0
			img.Pix[i+2] = 255 // magenta background
			img.Pix[i+3] = 255
		}
	}
	x0 := (w - inner) / 2
	y0 := (h - inner) / 2
	for y := y0; y < y0+inner; y++ {
		for x := x0; x < x0+inner; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = 220
			img.Pix[i+1] = 30
			img.Pix[i+2] = 30 // opaque red subject
			img.Pix[i+3] = 255
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func decodeNRGBA(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	return toNRGBA(img)
}

func TestRemoveChromaKeyBackground(t *testing.T) {
	in := decodeNRGBA(t, makeChromaSprite(64, 64, 20))
	out := removeChromaKeyBackground(in)

	// Corner (magenta) must be fully transparent.
	if a := out.Pix[out.PixOffset(0, 0)+3]; a != 0 {
		t.Errorf("expected transparent corner, got alpha=%d", a)
	}
	// Center (red subject) must stay opaque.
	if a := out.Pix[out.PixOffset(32, 32)+3]; a != 255 {
		t.Errorf("expected opaque center, got alpha=%d", a)
	}
}

func TestRemoveChromaKeyBackground_BelowThresholdKept(t *testing.T) {
	// Opaque red subject filling the frame, with only the 4 single-pixel corners
	// a different color. The corner-sampled background floods <1% of pixels, so
	// the safeguard must restore the original image unchanged (alpha all 255).
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = 200
		img.Pix[i+1] = 10
		img.Pix[i+2] = 10
		img.Pix[i+3] = 255
	}
	for _, c := range [4][2]int{{0, 0}, {31, 0}, {0, 31}, {31, 31}} {
		o := img.PixOffset(c[0], c[1])
		img.Pix[o] = 10
		img.Pix[o+1] = 10
		img.Pix[o+2] = 220 // distinct blue corners
	}
	out := removeChromaKeyBackground(img)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if a := out.Pix[out.PixOffset(x, y)+3]; a != 255 {
				t.Fatalf("below-threshold removal not restored at (%d,%d): alpha=%d", x, y, a)
			}
		}
	}
}

func TestNormalizeIcon(t *testing.T) {
	// 100x40 canvas, 10x10 opaque content at top-left corner.
	img := image.NewNRGBA(image.Rect(0, 0, 100, 40))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i+3] = 255
		}
	}
	out := normalizeIcon(img, 0.08)
	b := out.Bounds()
	if b.Dx() != b.Dy() {
		t.Errorf("expected square canvas, got %dx%d", b.Dx(), b.Dy())
	}
	// Tight crop: content (10) + padding on both sides should be far smaller
	// than the original 100px width.
	if b.Dx() >= 100 {
		t.Errorf("expected cropped output smaller than original width, got %d", b.Dx())
	}
}

func TestNormalizeIcon_NoContent(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16)) // fully transparent
	out := normalizeIcon(img, 0.08)
	if out.Bounds().Dx() != 16 || out.Bounds().Dy() != 16 {
		t.Errorf("expected original returned for empty image, got %v", out.Bounds())
	}
}

func TestAddWhiteOutline(t *testing.T) {
	// 20x20 transparent with a 6x6 opaque red square centered.
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20))
	for y := 7; y < 13; y++ {
		for x := 7; x < 13; x++ {
			i := img.PixOffset(x, y)
			img.Pix[i] = 200
			img.Pix[i+1] = 0
			img.Pix[i+2] = 0
			img.Pix[i+3] = 255
		}
	}
	out := addWhiteOutline(img, 3)

	// A pixel just outside the original square must now be white & opaque (outline).
	o := out.PixOffset(5, 10)
	r, g, bl, a := out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3]
	if a == 0 || !(r > 240 && g > 240 && bl > 240) {
		t.Errorf("expected white outline pixel at (5,10), got rgba=%d,%d,%d,%d", r, g, bl, a)
	}
	// The original subject pixel stays red.
	c := out.PixOffset(10, 10)
	if out.Pix[c] < 150 || out.Pix[c+1] > 60 {
		t.Errorf("expected red subject preserved at center, got rgb=%d,%d,%d", out.Pix[c], out.Pix[c+1], out.Pix[c+2])
	}
}

func TestPostProcessSprite_EndToEnd(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mobius-postproc-")
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "sprite.png")
	if err := os.WriteFile(path, makeChromaSprite(80, 80, 24), 0644); err != nil {
		t.Fatal(err)
	}

	if err := postProcessSprite(path); err != nil {
		t.Fatalf("postProcessSprite failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	out := decodeNRGBA(t, data)
	b := out.Bounds()
	if b.Dx() != b.Dy() {
		t.Errorf("expected square output after normalize, got %dx%d", b.Dx(), b.Dy())
	}
	// No magenta should survive anywhere.
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			i := out.PixOffset(x, y)
			if out.Pix[i+3] > 0 && out.Pix[i] == 255 && out.Pix[i+1] == 0 && out.Pix[i+2] == 255 {
				t.Fatalf("residual magenta at (%d,%d)", x, y)
			}
		}
	}
}

func TestPostProcessSprite_TooSmallNoop(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mobius-postproc-small-")
	defer os.RemoveAll(tmpDir)

	// 1x1 dummy PNG (the no-credentials placeholder shape).
	img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	path := filepath.Join(tmpDir, "dummy.png")
	os.WriteFile(path, buf.Bytes(), 0644)

	if err := postProcessSprite(path); err != nil {
		t.Errorf("expected no error on tiny image, got %v", err)
	}
}
