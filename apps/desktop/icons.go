package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"

	"github.com/egoist/mygo/plugins/sfsymbols"
	"github.com/egoist/mygo/ui"
)

func svgIcon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`,
	))
}

var (
	svgRefresh  = svgIcon(`<path d="M13 8a5 5 0 1 1-1.46-3.54"/><path d="M13 2.5v2.8h-2.8"/>`)
	svgGear     = svgIcon(`<circle cx="8" cy="8" r="2.2"/><path d="M8 2v1.5M8 12.5V14M2 8h1.5M12.5 8H14M3.76 3.76l1.06 1.06M11.18 11.18l1.06 1.06M12.24 3.76l-1.06 1.06M4.82 11.18l-1.06 1.06"/>`)
	svgFolder   = svgIcon(`<path d="M2 4.5A1.5 1.5 0 0 1 3.5 3h2.8l1.4 1.5H12.5A1.5 1.5 0 0 1 14 6v5.5a1.5 1.5 0 0 1-1.5 1.5h-9A1.5 1.5 0 0 1 2 11.5z"/>`)
	svgDoc      = svgIcon(`<path d="M4 2.5h5.2L12.5 5.8V13.5h-8.5z"/><path d="M9 2.5v3.5h3.5"/><path d="M6 8.5h4M6 11h2.6"/>`)
	svgDownload = svgIcon(`<path d="M8 2.5v7.5"/><path d="M5 7.5 8 10.5l3-3"/><path d="M3 13h10"/>`)
	svgCheck    = svgIcon(`<circle cx="8" cy="8" r="5.5"/><path d="m5.5 8.1 1.7 1.7 3.4-3.6"/>`)
	svgExternal = svgIcon(`<path d="M9.5 3H13v3.5"/><path d="M13 3 7.5 8.5"/><path d="M11.5 9v3a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V5.5a1 1 0 0 1 1-1h3"/>`)
	svgCalendar = svgIcon(`<rect x="2.5" y="3.5" width="11" height="10" rx="1.5"/><path d="M2.5 6.5h11M5.5 2v2.5M10.5 2v2.5"/>`)
	svgBack     = svgIcon(`<path d="M10 3.5 5.5 8l4.5 4.5"/>`)
	svgEye      = svgIcon(`<path d="M1.8 8s2.3-4 6.2-4 6.2 4 6.2 4-2.3 4-6.2 4-6.2-4-6.2-4z"/><circle cx="8" cy="8" r="1.7"/>`)
	svgSparkle  = svgIcon(`<path d="M8 2v12M2 8h12M4.5 4.5l7 7M11.5 4.5l-7 7"/>`)
)

// sym renders a crisp macOS SF Symbol with a clean SVG fallback for non-macOS or headless tests.
func sym(c *ui.Context, name string, size float32, col ui.Color, fallback *ui.SVG) sfsymbols.SymbolElement {
	return sfsymbols.Symbol(c, name).Size(size).Color(col).Fallback(fallback)
}

// trayIconPNG draws a sharp 36×36 (@2x Retina 18pt) monochrome document-with-slip
// template icon for the macOS menu bar.
func trayIconPNG() []byte {
	const size = 36
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	ink := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	if runtime.GOOS == "darwin" {
		ink = color.NRGBA{R: 0, G: 0, B: 0, A: 255}
	}

	// Draw a crisp rounded document silhouette with folded top-right corner and two ledger lines.
	setPixel := func(x, y int, alpha float64) {
		if x < 0 || y < 0 || x >= size || y >= size || alpha <= 0 {
			return
		}
		if alpha > 1 {
			alpha = 1
		}
		a := uint8(math.Round(alpha * 255))
		cur := img.NRGBAAt(x, y)
		if a > cur.A {
			img.SetNRGBA(x, y, color.NRGBA{R: ink.R, G: ink.G, B: ink.B, A: a})
		}
	}

	// Rounded rect SDF for outer document border (x: 7..29, y: 4..32, r: 3.5, stroke: 2.4).
	const (
		cx, cy = 18.0, 18.0
		hx, hy = 10.5, 13.5
		radius = 3.2
		stroke = 2.4
	)
	for y := range size {
		for x := range size {
			px, py := float64(x)+0.5, float64(y)+0.5
			// Skip folded corner cutout at top-right (px + py > 29 + 11 => chamfer)
			foldDist := (px - 22.0) + (10.5 - py)
			dx := math.Abs(px-cx) - (hx - radius)
			dy := math.Abs(py-cy) - (hy - radius)
			outside := math.Hypot(math.Max(dx, 0), math.Max(dy, 0))
			inside := math.Min(math.Max(dx, dy), 0)
			sdf := outside + inside - radius

			// Clip top-right corner cleanly with a 45° fold line.
			if px > 20.5 && py < 12.5 {
				diag := ((px - 21.5) - (py - 4.5)) / math.Sqrt2
				sdf = math.Max(sdf, diag)
			}

			// Stroke band: -stroke <= sdf <= 0
			distFromBand := math.Max(-sdf-stroke, sdf)
			cov := 0.5 - distFromBand
			if cov > 0 {
				setPixel(x, y, cov)
			}

			// Fold crease inner lines (x = 21..22.5, y = 4.5..11.5) and (y = 10.5..12.2, x = 21..28.5)
			if foldDist > -1.5 && px >= 20.5 && px <= 22.7 && py >= 4.5 && py <= 11.8 {
				setPixel(x, y, 1)
			}
			if py >= 10.0 && py <= 12.2 && px >= 21.0 && px <= 28.5 {
				setPixel(x, y, 1)
			}

			// Three clean payslip lines inside the document
			if px >= 12.0 && px <= 24.0 && py >= 16.5 && py <= 18.7 {
				setPixel(x, y, 1)
			}
			if px >= 12.0 && px <= 24.0 && py >= 21.2 && py <= 23.4 {
				setPixel(x, y, 1)
			}
			if px >= 12.0 && px <= 19.5 && py >= 25.9 && py <= 28.1 {
				setPixel(x, y, 1)
			}
		}
	}

	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
