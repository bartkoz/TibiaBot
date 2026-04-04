package vision

import (
	"image"

	"github.com/anthropics/tibiabot/internal/navigation"
	"gocv.io/x/gocv"
)

type MinimapLocator struct {
	atlas     *navigation.Atlas
	lastChunk *navigation.ChunkKey
	// Cache converted GoCV mats to avoid repeated conversion
	matCache map[navigation.ChunkKey]gocv.Mat
}

func NewMinimapLocator(atlas *navigation.Atlas) *MinimapLocator {
	return &MinimapLocator{
		atlas:    atlas,
		matCache: make(map[navigation.ChunkKey]gocv.Mat),
	}
}

// Close releases cached GoCV Mats.
func (ml *MinimapLocator) Close() {
	for _, mat := range ml.matCache {
		mat.Close()
	}
	ml.matCache = nil
}

// Locate finds the position of the minimap snippet within the atlas.
// Returns (x, y, confidence, found).
func (ml *MinimapLocator) Locate(snippet gocv.Mat, z int, confidenceThreshold ...float64) (int, int, float64, bool) {
	threshold := 0.7
	if len(confidenceThreshold) > 0 {
		threshold = confidenceThreshold[0]
	}

	snippetH := snippet.Rows()
	snippetW := snippet.Cols()

	mean, stddev := gocv.NewMat(), gocv.NewMat()
	defer mean.Close()
	defer stddev.Close()
	gocv.MeanStdDev(snippet, &mean, &stddev)
	if stddev.GetDoubleAt(0, 0) == 0 && stddev.GetDoubleAt(1, 0) == 0 && stddev.GetDoubleAt(2, 0) == 0 {
		return 0, 0, 0, false
	}

	var chunks []navigation.ChunkKey
	if ml.lastChunk != nil && ml.lastChunk.Z == z {
		chunks = ml.getNeighborChunks(*ml.lastChunk)
	} else {
		chunks = ml.atlas.ChunkKeysForZ(z)
	}

	bestScore := float32(-1.0)
	bestX, bestY := 0, 0
	var bestChunk *navigation.ChunkKey

	mask := gocv.NewMat()
	defer mask.Close()

	for _, ck := range chunks {
		tile := ml.getTileMat(ck)
		if tile.Empty() {
			continue
		}
		if tile.Rows() < snippetH || tile.Cols() < snippetW {
			continue
		}

		result := gocv.NewMat()
		gocv.MatchTemplate(tile, snippet, &result, gocv.TmCcoeffNormed, mask)
		_, maxVal, _, maxLoc := gocv.MinMaxLoc(result)
		result.Close()

		if maxVal > bestScore {
			bestScore = maxVal
			bestX = ck.X + maxLoc.X
			bestY = ck.Y + maxLoc.Y
			c := ck
			bestChunk = &c
		}
	}

	if bestScore < float32(threshold) {
		if ml.lastChunk != nil {
			ml.lastChunk = nil
			return ml.Locate(snippet, z, threshold)
		}
		return 0, 0, 0, false
	}

	ml.lastChunk = bestChunk
	return bestX, bestY, float64(bestScore), true
}

func (ml *MinimapLocator) getTileMat(ck navigation.ChunkKey) gocv.Mat {
	if mat, ok := ml.matCache[ck]; ok {
		return mat
	}
	tile, ok := ml.atlas.GetColorTile(ck.X, ck.Y, ck.Z)
	if !ok {
		return gocv.NewMat()
	}
	mat := rgbaToMat(tile)
	ml.matCache[ck] = mat
	return mat
}

// rgbaToMat converts an image.RGBA to a GoCV Mat (RGB, 3-channel).
func rgbaToMat(img *image.RGBA) gocv.Mat {
	bounds := img.Bounds()
	rows := bounds.Dy()
	cols := bounds.Dx()
	mat := gocv.NewMatWithSize(rows, cols, gocv.MatTypeCV8UC3)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			off := y*img.Stride + x*4
			r := img.Pix[off+0]
			g := img.Pix[off+1]
			b := img.Pix[off+2]
			mat.SetUCharAt(y, x*3+0, r)
			mat.SetUCharAt(y, x*3+1, g)
			mat.SetUCharAt(y, x*3+2, b)
		}
	}
	return mat
}

func (ml *MinimapLocator) getNeighborChunks(center navigation.ChunkKey) []navigation.ChunkKey {
	var neighbors []navigation.ChunkKey
	cs := navigation.ChunkSize
	for dx := -cs; dx <= cs; dx += cs {
		for dy := -cs; dy <= cs; dy += cs {
			key := navigation.ChunkKey{X: center.X + dx, Y: center.Y + dy, Z: center.Z}
			if _, ok := ml.atlas.ColorChunks[key]; ok {
				neighbors = append(neighbors, key)
			}
		}
	}
	return neighbors
}

