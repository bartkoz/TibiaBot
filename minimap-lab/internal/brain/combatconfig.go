package brain

import (
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"minimap-lab/internal/battle"
	"minimap-lab/internal/vision"
	"minimap-lab/internal/vitals"
)

// Rect is a rectangle in the shared screen's pixels, the way the panel
// measures it by dragging.
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (r Rect) Empty() bool             { return r.W <= 0 || r.H <= 0 }
func (r Rect) Bounds() image.Rectangle { return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H) }

// CombatConfig is everything the vision layer needs. It is nested rather than
// flattened into Config because it is a dozen fields that only make sense
// together, and because an empty value has to mean "not calibrated yet"
// rather than "a bar zero pixels wide".
type CombatConfig struct {
	// Viewport is the whole game window; Crop is the part of it the panel
	// actually sends. Crop is an explicit field rather than a formula so the
	// panel and the brain cannot drift by a pixel: the panel offers a button
	// that fills it from RecommendedCrop, but the value lives here.
	Viewport Rect `json:"viewport"`
	Crop     Rect `json:"crop"`
	Battle   Rect `json:"battle"`
	HP       Rect `json:"hp"`
	Mana     Rect `json:"mana"`

	// GridCols and GridRows are the tiles the game window shows. 15x11 in the
	// official client, overridable because that is an assumption about a
	// client build, not a law.
	GridCols int `json:"grid_cols"`
	GridRows int `json:"grid_rows"`

	BarWidth     int      `json:"bar_width"`
	BarHeight    int      `json:"bar_height"`
	BarBorder    int      `json:"bar_border"`
	BarTolerance int      `json:"bar_tolerance"`
	BlackMax     int      `json:"black_max"`
	BarColors    []string `json:"bar_colors"`

	// HasSelfBar says the client draws the character's own bar. When it does,
	// AnchorDX and AnchorDY are ignored and derived from SelfBarX/SelfBarY
	// instead: the character's bar belongs to a creature on a tile we know
	// exactly, so it measures the anchor for us. The explicit fields are the
	// fallback for a client with the own bar switched off.
	HasSelfBar bool    `json:"has_self_bar"`
	SelfBarX   int     `json:"self_bar_x"`
	SelfBarY   int     `json:"self_bar_y"`
	AnchorDX   float64 `json:"anchor_dx"`
	AnchorDY   float64 `json:"anchor_dy"`

	// DecisionRadius is the upper bound on everything: it sizes the crop and
	// it is the radius the snapshot counts creatures in. A spell rule may not
	// ask for more, or it would be asking about tiles the panel never sent.
	DecisionRadius float64 `json:"decision_radius"`

	BattleBarWidth       int     `json:"battle_bar_width"`
	BattleBarHeight      int     `json:"battle_bar_height"`
	BattleBarBorder      int     `json:"battle_bar_border"`
	BattleRowPitch       int     `json:"battle_row_pitch"`
	BattleFrame          string  `json:"battle_frame"`
	BattleFrameTolerance int     `json:"battle_frame_tolerance"`
	BattleFrameCoverage  float64 `json:"battle_frame_coverage"`
}

// Enabled reports whether there is enough calibration to look at anything.
func (c CombatConfig) Enabled() bool { return !c.Viewport.Empty() && !c.Crop.Empty() }

// withDefaults fills the fields the panel may leave out. Zero is treated as
// "unset" for each of them, which is safe because zero is not a legal value
// for any of them either: GridCols/GridRows/DecisionRadius/BattleFrameCoverage
// all have a validated minimum above zero, BarTolerance/BlackMax/
// BattleFrameTolerance are now validated as 1-128 rather than 0-128 for
// exactly this reason, and an empty colour list would mean "find nothing".
func (c CombatConfig) withDefaults() CombatConfig {
	if c.GridCols == 0 {
		c.GridCols = 15
	}
	if c.GridRows == 0 {
		c.GridRows = 11
	}
	if c.DecisionRadius == 0 {
		c.DecisionRadius = 4
	}
	if c.BarTolerance == 0 {
		c.BarTolerance = 12
	}
	if c.BlackMax == 0 {
		c.BlackMax = 48
	}
	if len(c.BarColors) == 0 {
		for _, col := range vision.DefaultColors() {
			c.BarColors = append(c.BarColors, fmt.Sprintf("#%02x%02x%02x", col.R, col.G, col.B))
		}
	}
	if c.BattleFrameTolerance == 0 {
		c.BattleFrameTolerance = 12
	}
	if c.BattleFrameCoverage == 0 {
		c.BattleFrameCoverage = 0.8
	}
	return c
}

// tileSize is pixels per tile, derived from the game window rather than
// configured: the official client always shows the same number of tiles, so a
// separate tile size field could only ever disagree with the rectangle.
func (c CombatConfig) tileSize() (w, h float64) {
	return float64(c.Viewport.W) / float64(c.GridCols), float64(c.Viewport.H) / float64(c.GridRows)
}

// RecommendedCrop is the character's tile grown by the decision radius plus one
// tile of margin, clipped to the game window. The extra tile is what makes a
// creature at the very edge of the radius still show its whole health bar
// inside the crop - a clipped bar is not detected at all.
func (c CombatConfig) RecommendedCrop() Rect {
	c = c.withDefaults()
	if c.Viewport.Empty() {
		return Rect{}
	}
	tw, th := c.tileSize()
	reach := int(math.Ceil(c.DecisionRadius)) + 1
	col, row := c.GridCols/2, c.GridRows/2
	x0, x1 := max(0, col-reach), min(c.GridCols, col+reach+1)
	y0, y1 := max(0, row-reach), min(c.GridRows, row+reach+1)
	return Rect{
		X: c.Viewport.X + int(math.Round(float64(x0)*tw)),
		Y: c.Viewport.Y + int(math.Round(float64(y0)*th)),
		W: int(math.Round(float64(x1-x0) * tw)),
		H: int(math.Round(float64(y1-y0) * th)),
	}
}

func (c CombatConfig) validate() error {
	if !c.Enabled() {
		// Not calibrated is a legal state: the panel is meant to be
		// calibrated one rectangle at a time, checking each as it goes.
		if c.Viewport.Empty() && c.Crop.Empty() {
			return nil
		}
		return fmt.Errorf("okno gry i wycinek trzeba zaznaczyć razem")
	}
	if c.GridCols < 3 || c.GridCols > 64 || c.GridRows < 3 || c.GridRows > 64 {
		return fmt.Errorf("siatka kratek musi mieścić się w zakresie 3–64 w obu wymiarach")
	}
	if c.Viewport.W < c.GridCols || c.Viewport.H < c.GridRows {
		return fmt.Errorf("okno gry jest mniejsze niż jedna kratka na kolumnę")
	}
	if !c.Crop.Bounds().In(c.Viewport.Bounds()) {
		return fmt.Errorf("wycinek musi mieścić się w oknie gry")
	}
	// The radius is checked before it is used to derive RecommendedCrop: a
	// nonsensical radius must be reported as such, not as a crop that
	// mysteriously fails to cover it.
	if c.DecisionRadius < 0.5 || c.DecisionRadius > 16 {
		return fmt.Errorf("promień decyzji musi mieścić się w zakresie 0,5–16 kratek")
	}
	if want := c.RecommendedCrop(); !want.Bounds().In(c.Crop.Bounds()) {
		return fmt.Errorf("wycinek jest za mały na promień decyzji: potrzebne co najmniej %d×%d px od %d,%d",
			want.W, want.H, want.X, want.Y)
	}
	if err := checkBar("paska życia", c.BarWidth, c.BarHeight, c.BarBorder); err != nil {
		return err
	}
	if c.BarTolerance < 1 || c.BarTolerance > 128 || c.BlackMax < 1 || c.BlackMax > 128 {
		return fmt.Errorf("tolerancja barw i próg czerni muszą mieścić się w zakresie 1–128")
	}
	for _, s := range c.BarColors {
		col, err := parseColor(s)
		if err != nil {
			return fmt.Errorf("barwa paska %q: %w", s, err)
		}
		// A colour whose darkest channel, once Tolerance is subtracted, still
		// falls at or below BlackMax is simultaneously a fill colour and a
		// dark one: fillRun would terminate after a single pixel, and every
		// bar using that colour would read as roughly 4% full regardless of
		// its true fill. This must be refused outright, because vision.Find
		// has no channel to report the problem - it would just find nothing,
		// which reads as "no monsters", the most dangerous possible mistake.
		if mc := maxChannel(col); mc-c.BarTolerance <= c.BlackMax {
			return fmt.Errorf(
				"barwa paska %q: próg czerni %d razem z tolerancją %d pochłania jej wypełnienie (największy kanał %d)",
				s, c.BlackMax, c.BarTolerance, mc)
		}
	}
	if !c.Battle.Empty() {
		if err := checkBar("paska w battle liście", c.BattleBarWidth, c.BattleBarHeight, c.BattleBarBorder); err != nil {
			return err
		}
		if c.BattleRowPitch < c.BattleBarHeight || c.BattleRowPitch > 256 {
			return fmt.Errorf("odstęp wierszy battle listy musi być nie mniejszy niż wysokość paska i nie większy niż 256 px")
		}
		if _, err := parseColor(c.BattleFrame); err != nil {
			return fmt.Errorf("barwa ramki celu %q: %w", c.BattleFrame, err)
		}
		if c.BattleFrameTolerance < 1 || c.BattleFrameTolerance > 128 {
			return fmt.Errorf("tolerancja ramki celu musi mieścić się w zakresie 1–128")
		}
		if c.BattleFrameCoverage < 0.05 || c.BattleFrameCoverage > 1 {
			return fmt.Errorf("pokrycie ramki celu musi mieścić się w zakresie 0,05–1")
		}
	}
	if !c.HP.Empty() && (c.HP.W < 8 || c.HP.H < 1) {
		return fmt.Errorf("prostokąt paska HP musi mieć co najmniej 8 px szerokości")
	}
	if !c.Mana.Empty() && (c.Mana.W < 8 || c.Mana.H < 1) {
		return fmt.Errorf("prostokąt paska many musi mieć co najmniej 8 px szerokości")
	}
	return nil
}

func checkBar(what string, w, h, border int) error {
	if w < 3 || w > 256 || h < 3 || h > 64 {
		return fmt.Errorf("wymiary %s muszą mieścić się w zakresie 3–256 na 3–64 px", what)
	}
	if border < 1 || border > 8 || 2*border >= w || 2*border >= h {
		return fmt.Errorf("obwódka %s musi mieć 1–8 px i zostawić miejsce na wypełnienie", what)
	}
	return nil
}

// parseColor reads "#rrggbb". The panel sends colours as text because that is
// what a colour input produces and what a human can retype from a screenshot.
func parseColor(s string) (vision.Color, error) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return vision.Color{}, fmt.Errorf("oczekiwano zapisu #rrggbb")
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return vision.Color{}, fmt.Errorf("oczekiwano zapisu #rrggbb")
	}
	return vision.Color{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}, nil
}

// maxChannel is the largest of a colour's three channels. isDark only fires
// once every channel is at or below BlackMax, so the largest channel is the
// one hardest to push below that line - it is the one that decides whether a
// fill pixel of this colour can ever also read as dark.
func maxChannel(c vision.Color) int {
	m := c.R
	if c.G > m {
		m = c.G
	}
	if c.B > m {
		m = c.B
	}
	return int(m)
}

func (c CombatConfig) geometry() vision.Geometry {
	return vision.Geometry{Width: c.BarWidth, Height: c.BarHeight, Border: c.BarBorder}
}

// barOptions is only ever called on a validated config, so the colours parse.
func (c CombatConfig) barOptions() (vision.Options, error) {
	o := vision.Options{
		Geometry: c.geometry(), Tolerance: c.BarTolerance,
		BlackMax: c.BlackMax, ExcludeTolerance: 2,
	}
	for _, s := range c.BarColors {
		col, err := parseColor(s)
		if err != nil {
			return vision.Options{}, err
		}
		o.Colors = append(o.Colors, col)
	}
	if c.HasSelfBar {
		o.Exclude = []image.Point{{X: c.SelfBarX, Y: c.SelfBarY}}
	}
	return o, nil
}

func (c CombatConfig) grid() vision.Grid {
	tw, th := c.tileSize()
	g := vision.Grid{
		Cols: c.GridCols, Rows: c.GridRows, TileW: tw, TileH: th,
		CropX:    float64(c.Crop.X - c.Viewport.X),
		CropY:    float64(c.Crop.Y - c.Viewport.Y),
		Geometry: c.geometry(),
		AnchorDX: c.AnchorDX, AnchorDY: c.AnchorDY,
	}
	if c.HasSelfBar {
		g.AnchorDX, g.AnchorDY = g.AnchorFrom(vision.Bar{X: c.SelfBarX, Y: c.SelfBarY})
	}
	return g
}

func (c CombatConfig) battleOptions() (battle.Options, error) {
	frame, err := parseColor(c.BattleFrame)
	if err != nil {
		return battle.Options{}, err
	}
	o := battle.Options{
		Geometry: vision.Geometry{Width: c.BattleBarWidth, Height: c.BattleBarHeight,
			Border: c.BattleBarBorder},
		Tolerance: c.BarTolerance, BlackMax: c.BlackMax, RowPitch: c.BattleRowPitch,
		Frame: frame, FrameTolerance: c.BattleFrameTolerance,
		FrameCoverage: c.BattleFrameCoverage,
	}
	for _, s := range c.BarColors {
		col, err := parseColor(s)
		if err != nil {
			return battle.Options{}, err
		}
		o.Colors = append(o.Colors, col)
	}
	return o, nil
}

func (c CombatConfig) vitalsOptions() vitals.Options { return vitals.DefaultOptions() }
