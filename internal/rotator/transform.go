package rotator

import "math"

type Position struct {
	Azimuth   float64
	Elevation float64
}

type CameraPosition struct {
	Pan  float64
	Tilt float64
}

type Transform struct {
	PanOffset   float64
	PanScale    float64
	TiltHorizon float64
	TiltScale   float64
}

func (t Transform) ToCamera(pos Position) CameraPosition {
	pan := normalize360(t.PanOffset + t.PanScale*pos.Azimuth)
	// atomcam_tools accepts 0..355 although the product is mechanically close to 360 degrees.
	// Map the five degree protocol gap to its nearest endpoint.
	if pan > 355 {
		if pan >= 357.5 {
			pan = 0
		} else {
			pan = 355
		}
	}
	tilt := clamp(t.TiltHorizon+t.TiltScale*pos.Elevation, 0, 180)
	return CameraPosition{Pan: pan, Tilt: tilt}
}

func (t Transform) FromCamera(pos CameraPosition) Position {
	azimuth := normalize360((pos.Pan - t.PanOffset) / t.PanScale)
	elevation := clamp((pos.Tilt-t.TiltHorizon)/t.TiltScale, 0, 90)
	return Position{Azimuth: azimuth, Elevation: elevation}
}

func angularDistance(a, b float64) float64 {
	difference := math.Abs(normalize360(a) - normalize360(b))
	return math.Min(difference, 360-difference)
}

func normalize360(value float64) float64 {
	value = math.Mod(value, 360)
	if value < 0 {
		value += 360
	}
	return value
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}
