package tools

// Sprite post-processing ported from the Python reference tools/media/bg_remover.py
// (the non-ML parts). Native Go image ops, no third-party dependencies.
//
//   - removeChromaKeyBackground: corner flood-fill + edge-connected chroma cleanup,
//     turning a flat chroma-key/solid background transparent.
//   - normalizeIcon: crop to the content bounding box (+padding) and center on a
//     square transparent canvas.
//   - addWhiteOutline: dilate the alpha mask and composite the sprite over a white
//     silhouette.
//
// rembg (u2net) salient-object segmentation is intentionally NOT ported: it is a
// PyTorch ML model with no native Go equivalent. Sprites must therefore be
// generated on a flat chroma-key background for removal to work.

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
)

const (
	chromaTolerance      = 40   // per-channel color distance (summed) threshold base
	chromaMinRemovedFrac = 0.01 // if <1% removed, background wasn't solid -> keep original
	outlineStrokeWidth   = 3
	iconPaddingPct       = 0.08
	contentAlphaThresh   = 10 // alpha above this counts as content for bbox
)

// chromaKeys are the flat background colors the asset prompts render, cleared on
// the edge-connected second pass (magenta/cyan/neon-pink/lime/green variants).
var chromaKeys = [6][3]int{
	{255, 0, 255}, {0, 255, 255}, {255, 0, 170},
	{170, 255, 0}, {0, 255, 0}, {0, 248, 0},
}

func ipAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func ipMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ipMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// postProcessSprite removes the chroma-key background, normalizes the icon to a
// tight square crop, and adds a white outline, overwriting the file at path.
// Images too small to be real sprites (e.g. placeholder PNGs) are left untouched.
func postProcessSprite(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	b := img.Bounds()
	if b.Dx() < 4 || b.Dy() < 4 {
		return nil // too small to process (placeholder / dummy)
	}

	out := removeChromaKeyBackground(toNRGBA(img))
	out = normalizeIcon(out, iconPaddingPct)
	out = addWhiteOutline(out, outlineStrokeWidth)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

// removeChromaKeyBackground floods the background transparent. Returns the image
// unchanged when too little is removed (the background was not flat/chroma-key).
func removeChromaKeyBackground(src *image.NRGBA) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return src
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	copy(out.Pix, src.Pix)

	rgbAt := func(x, y int) (int, int, int) {
		i := out.PixOffset(x, y)
		return int(out.Pix[i]), int(out.Pix[i+1]), int(out.Pix[i+2])
	}

	corners := [4][2]int{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}}
	var br, bg, bb int
	for _, c := range corners {
		r, g, bl := rgbAt(c[0], c[1])
		br += r
		bg += g
		bb += bl
	}
	br /= 4
	bg /= 4
	bb /= 4
	isBg := func(r, g, bl int) bool {
		return ipAbs(r-br)+ipAbs(g-bg)+ipAbs(bl-bb) < chromaTolerance*3
	}

	visited := make([]bool, w*h)
	stack := make([][2]int, 0, w)
	for _, c := range corners {
		r, g, bl := rgbAt(c[0], c[1])
		idx := c[1]*w + c[0]
		if isBg(r, g, bl) && !visited[idx] {
			visited[idx] = true
			stack = append(stack, c)
		}
	}

	removed := 0
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := p[0], p[1]
		r, g, bl := rgbAt(x, y)
		if !isBg(r, g, bl) {
			continue
		}
		out.Pix[out.PixOffset(x, y)+3] = 0
		removed++
		for _, n := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
			nx, ny := n[0], n[1]
			if nx >= 0 && nx < w && ny >= 0 && ny < h {
				idx := ny*w + nx
				if !visited[idx] {
					visited[idx] = true
					stack = append(stack, n)
				}
			}
		}
	}

	removed += edgeChromaCleanup(out)

	if removed < int(float64(w*h)*chromaMinRemovedFrac) {
		copy(out.Pix, src.Pix) // background wasn't solid — restore original
	}
	return out
}

// edgeChromaCleanup clears chroma-key colored pixels connected to the image edge
// (handles layered backgrounds: solid border + chroma interior) without punching
// holes through sprite interiors that legitimately share a chroma hue.
func edgeChromaCleanup(out *image.NRGBA) int {
	b := out.Bounds()
	w, h := b.Dx(), b.Dy()
	isChroma := func(r, g, bl int) bool {
		for _, c := range chromaKeys {
			if ipAbs(r-c[0])+ipAbs(g-c[1])+ipAbs(bl-c[2]) < chromaTolerance*3 {
				return true
			}
		}
		return false
	}

	visited := make([]bool, w*h)
	stack := make([][2]int, 0, 2*(w+h))
	push := func(x, y int) {
		if x < 0 || x >= w || y < 0 || y >= h {
			return
		}
		idx := y*w + x
		if !visited[idx] {
			visited[idx] = true
			stack = append(stack, [2]int{x, y})
		}
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 0; y < h; y++ {
		push(0, y)
		push(w-1, y)
	}

	residual := 0
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := p[0], p[1]
		i := out.PixOffset(x, y)
		if out.Pix[i+3] == 0 {
			push(x-1, y)
			push(x+1, y)
			push(x, y-1)
			push(x, y+1)
			continue
		}
		r, g, bl := int(out.Pix[i]), int(out.Pix[i+1]), int(out.Pix[i+2])
		if !isChroma(r, g, bl) {
			continue
		}
		out.Pix[i+3] = 0
		residual++
		push(x-1, y)
		push(x+1, y)
		push(x, y-1)
		push(x, y+1)
	}
	return residual
}

// normalizeIcon crops to the content bounding box (plus padding) and centers it
// on a square transparent canvas. Resolution is preserved (no downscale).
func normalizeIcon(src *image.NRGBA, paddingPct float64) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	minX, minY, maxX, maxY := w, h, -1, -1
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if src.Pix[src.PixOffset(x, y)+3] > contentAlphaThresh {
				minX = ipMin(minX, x)
				minY = ipMin(minY, y)
				maxX = ipMax(maxX, x)
				maxY = ipMax(maxY, y)
			}
		}
	}
	if maxX < minX || maxY < minY {
		return src // no content
	}

	cw := maxX - minX + 1
	ch := maxY - minY + 1
	pad := int(float64(ipMax(cw, ch)) * paddingPct)
	x1 := ipMax(0, minX-pad)
	y1 := ipMax(0, minY-pad)
	x2 := ipMin(w, maxX+pad+1)
	y2 := ipMin(h, maxY+pad+1)
	cropW := x2 - x1
	cropH := y2 - y1

	side := ipMax(cropW, cropH)
	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	offX := (side - cropW) / 2
	offY := (side - cropH) / 2
	draw.Draw(dst, image.Rect(offX, offY, offX+cropW, offY+cropH), src, image.Pt(x1, y1), draw.Src)
	return dst
}

// addWhiteOutline dilates the alpha mask and composites the sprite (src-over) on
// top of the resulting white silhouette.
func addWhiteOutline(src *image.NRGBA, stroke int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	alpha := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			alpha[y*w+x] = src.Pix[src.PixOffset(x, y)+3]
		}
	}
	for s := 0; s < stroke; s++ {
		alpha = maxFilter3(alpha, w, h)
	}

	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if alpha[y*w+x] > 0 {
				o := out.PixOffset(x, y)
				out.Pix[o] = 255
				out.Pix[o+1] = 255
				out.Pix[o+2] = 255
				out.Pix[o+3] = 255
			}
		}
	}

	// Composite original over the white silhouette: result = src over dst.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := src.PixOffset(x, y)
			sa := src.Pix[i+3]
			if sa == 0 {
				continue
			}
			o := out.PixOffset(x, y)
			if sa == 255 {
				out.Pix[o] = src.Pix[i]
				out.Pix[o+1] = src.Pix[i+1]
				out.Pix[o+2] = src.Pix[i+2]
				out.Pix[o+3] = 255
				continue
			}
			af := float64(sa) / 255
			for c := 0; c < 3; c++ {
				out.Pix[o+c] = uint8(float64(src.Pix[i+c])*af + float64(out.Pix[o+c])*(1-af))
			}
			da := out.Pix[o+3]
			out.Pix[o+3] = uint8(float64(sa) + float64(da)*(1-af))
		}
	}
	return out
}

// maxFilter3 returns a 3x3 maximum (dilation) of a single-channel mask.
func maxFilter3(srcMask []uint8, w, h int) []uint8 {
	dst := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var m uint8
			for dy := -1; dy <= 1; dy++ {
				ny := y + dy
				if ny < 0 || ny >= h {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					nx := x + dx
					if nx < 0 || nx >= w {
						continue
					}
					if v := srcMask[ny*w+nx]; v > m {
						m = v
					}
				}
			}
			dst[y*w+x] = m
		}
	}
	return dst
}
