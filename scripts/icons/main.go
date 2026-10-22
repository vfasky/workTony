// Command icons makes the app's icons from assets/icon-master.png: the
// app's, its artwork scaled to the macOS icon grid over a soft shadow, and
// the development app's, the same with a DEBUG badge, which mygo dev uses
// and mygo build leaves out.
//
//	go run ./scripts/icons
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const (
	size = 1024
	// body is the artwork's size in the macOS icon grid: 824 of 1024, so
	// the icon is as large as the others in the Dock.
	body = 824
)

func main() {
	master := load("assets/icon-master.png")
	art := crop(master)
	w, h := art.Bounds().Dx(), art.Bounds().Dy()
	scale := float64(body) / float64(max(w, h))
	sw, sh := int(float64(w)*scale+0.5), int(float64(h)*scale+0.5)
	scaled := image.NewNRGBA(image.Rect(0, 0, sw, sh))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), art, art.Bounds(), draw.Src, nil)

	at := image.Pt((size-sw)/2, (size-sh)/2)
	icon := image.NewNRGBA(image.Rect(0, 0, size, size))
	shadow(icon, scaled, at.Add(image.Pt(0, 10)), 14, 0.30)
	draw.Draw(icon, scaled.Bounds().Add(at), scaled, image.Point{}, draw.Over)
	save("resources/icon.png", icon)

	dev := image.NewNRGBA(icon.Bounds())
	draw.Draw(dev, dev.Bounds(), icon, image.Point{}, draw.Src)
	badge(dev, image.Rect(at.X+sw/2-300, at.Y+sh-180, at.X+sw/2+300, at.Y+sh-24), "DEBUG")
	save("resources/icon-dev.png", dev)
}

// crop cuts img to what it shows.
func crop(img image.Image) image.Image {
	b := img.Bounds()
	r := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				r = r.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	out := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), img, r.Min, draw.Src)
	return out
}

// shadow draws the blurred shape of src at at onto dst, opacity strong.
func shadow(dst *image.NRGBA, src image.Image, at image.Point, radius int, opacity float64) {
	n := dst.Bounds().Dx()
	alpha := make([]float64, n*n)
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx, dy := x-b.Min.X+at.X, y-b.Min.Y+at.Y
			if dx < 0 || dy < 0 || dx >= n || dy >= n {
				continue
			}
			_, _, _, a := src.At(x, y).RGBA()
			alpha[dy*n+dx] = float64(a) / 0xffff
		}
	}
	// Three box blurs make a Gaussian.
	for range 3 {
		alpha = boxBlur(alpha, n, radius, true)
		alpha = boxBlur(alpha, n, radius, false)
	}
	shade := image.NewNRGBA(dst.Bounds())
	for i, a := range alpha {
		shade.Pix[i*4+3] = uint8(min(255, a*opacity*255+0.5))
	}
	draw.Draw(dst, dst.Bounds(), shade, image.Point{}, draw.Over)
}

func boxBlur(src []float64, n, r int, horizontal bool) []float64 {
	out := make([]float64, len(src))
	at := func(line, i int) int {
		if horizontal {
			return line*n + i
		}
		return i*n + line
	}
	width := float64(2*r + 1)
	for line := range n {
		sum := 0.0
		for i := -r; i <= r; i++ {
			if i >= 0 && i < n {
				sum += src[at(line, i)]
			}
		}
		for i := range n {
			out[at(line, i)] = sum / width
			if j := i - r; j >= 0 {
				sum -= src[at(line, j)]
			}
			if j := i + r + 1; j < n {
				sum += src[at(line, j)]
			}
		}
	}
	return out
}

// badge draws a pill in r with text on it, edged in white over a shadow.
func badge(dst *image.NRGBA, r image.Rectangle, text string) {
	edge := 12
	outer := r.Inset(-edge)
	pillShape := image.NewNRGBA(dst.Bounds())
	fillPill(pillShape, outer, color.NRGBA{0, 0, 0, 255})
	shadow(dst, pillShape, image.Pt(0, 8), 10, 0.35)
	fillPill(dst, outer, color.NRGBA{255, 255, 255, 255})
	fillPill(dst, r, color.NRGBA{0xF9, 0x73, 0x16, 255})

	f, err := opentype.Parse(gobold.TTF)
	if err != nil {
		log.Fatal(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 96, DPI: 72, Hinting: font.HintingNone})
	if err != nil {
		log.Fatal(err)
	}
	defer face.Close()
	tracking := fixed.I(10)
	width := fixed.Int26_6(0)
	for i, ch := range text {
		adv, _ := face.GlyphAdvance(ch)
		width += adv
		if i > 0 {
			width += tracking
		}
	}
	m := face.Metrics()
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(color.White), Face: face}
	x := fixed.I(r.Min.X+r.Dx()/2) - width/2
	y := fixed.I(r.Min.Y+r.Dy()/2) + (m.Ascent-m.Descent)/2 - m.Descent/4
	for _, ch := range text {
		d.Dot = fixed.Point26_6{X: x, Y: y}
		d.DrawString(string(ch))
		adv, _ := face.GlyphAdvance(ch)
		x += adv + tracking
	}
}

// fillPill fills the pill r, its ends round, with c.
func fillPill(dst *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	b := dst.Bounds()
	z := vector.NewRasterizer(b.Dx(), b.Dy())
	x0, y0, x1, y1 := float32(r.Min.X), float32(r.Min.Y), float32(r.Max.X), float32(r.Max.Y)
	rad := (y1 - y0) / 2
	k := rad * 0.5523 // cubic Béziers as quarter circles
	z.MoveTo(x0+rad, y0)
	z.LineTo(x1-rad, y0)
	z.CubeTo(x1-rad+k, y0, x1, y0+rad-k, x1, y0+rad)
	z.CubeTo(x1, y0+rad+k, x1-rad+k, y1, x1-rad, y1)
	z.LineTo(x0+rad, y1)
	z.CubeTo(x0+rad-k, y1, x0, y0+rad+k, x0, y0+rad)
	z.CubeTo(x0, y0+rad-k, x0+rad-k, y0, x0+rad, y0)
	z.ClosePath()
	z.Draw(dst, b, image.NewUniform(c), image.Point{})
}

func load(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		log.Fatal(err)
	}
	return img
}

func save(path string, img image.Image) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote", path)
}
