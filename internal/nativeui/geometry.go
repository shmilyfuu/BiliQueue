package nativeui

import "math"

type Rect struct {
	X int
	Y int
	W int
	H int
}

func (r Rect) Contains(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (r Rect) Inset(value int) Rect {
	return Rect{X: r.X + value, Y: r.Y + value, W: maxInt(0, r.W-2*value), H: maxInt(0, r.H-2*value)}
}

func (r Rect) inset(value int) Rect { return r.Inset(value) }

func (d Design) LayoutRect(path string, rect Rect) Rect {
	if delta, ok := d.Layout.Offsets[path]; ok {
		rect.X += delta.X
		rect.Y += delta.Y
	}
	if delta, ok := d.Layout.SizeDeltas[path]; ok {
		rect.W = maxInt(1, rect.W+delta.W)
		rect.H = maxInt(1, rect.H+delta.H)
	}
	return rect
}

func clampInt(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func linearGradientHalfVector(width, height float64, angle int) (float64, float64) {
	radians := float64(angle) * math.Pi / 180
	dx, dy := math.Cos(radians), math.Sin(radians)
	scale := math.Abs(width*dx) + math.Abs(height*dy)
	return dx * scale / 2, dy * scale / 2
}

func radialGradientGeometry(width, height float64, centerX, centerY, radius int, shape string) (cx, cy, rx, ry float64) {
	cx = width * float64(clampInt(centerX, 0, 100)) / 100
	cy = height * float64(clampInt(centerY, 0, 100)) / 100
	scale := float64(maxInt(1, radius)) / 100
	if shape == "circle" {
		base := math.Max(width, height)
		return cx, cy, base * scale, base * scale
	}
	return cx, cy, width * scale, height * scale
}
