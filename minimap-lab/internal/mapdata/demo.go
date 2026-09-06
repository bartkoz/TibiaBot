package mapdata

import (
	"image"
	"image/color"
	"math/rand"
)

// The demo atlas is synthetic map data, which is why it lives here rather than
// with the HTTP handler that serves it: the panel's demo button and the
// matcher's own tests both need it, and neither should have to reach into the
// other for a fixture.

// DemoAtlas builds a deterministic patch of fake terrain - noise, a river and
// a road - anchored at world (32000,32000,7). It shows the algorithm working
// without any downloaded map pack; it proves nothing about the real client.
func DemoAtlas() *Atlas {
	im := image.NewNRGBA(image.Rect(0, 0, 384, 320))
	rng := rand.New(rand.NewSource(42))
	palette := []color.NRGBA{{30, 95, 40, 255}, {50, 125, 45, 255}, {80, 145, 55, 255}, {100, 100, 100, 255}, {160, 140, 70, 255}}
	for y := 0; y < 320; y++ {
		for x := 0; x < 384; x++ {
			c := palette[rng.Intn(len(palette))]
			if abs(x-90-y/3) < 7 {
				c = color.NRGBA{0, 100, 210, 255}
			}
			if y > 155 && y < 162 {
				c = color.NRGBA{190, 170, 105, 255}
			}
			im.SetNRGBA(x, y, c)
		}
	}
	return &Atlas{im, image.Pt(32000, 32000), 7}
}

// DemoSnippet crops DemoAtlas the way the game would show it on the minimap.
func DemoSnippet(a *Atlas) image.Image {
	// A 95×95-tile crop at zoom 2; player is at world (32200,32180,7).
	im := image.NewNRGBA(image.Rect(0, 0, 190, 190))
	for y := 0; y < 190; y++ {
		for x := 0; x < 190; x++ {
			im.Set(x, y, a.Image.At(153+x/2, 133+y/2))
		}
	}
	for d := -5; d <= 5; d++ {
		im.Set(94+d, 94, color.White)
		im.Set(94, 94+d, color.White)
	}
	return im
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
