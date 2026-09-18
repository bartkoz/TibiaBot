package brain

import (
	"strings"
	"testing"

	"minimap-lab/internal/vision"
)

// calibrated is a whole, valid calibration: a 240x176 game window at 16px per
// tile, cropped to the middle 11x11 tiles.
func calibrated() CombatConfig {
	return CombatConfig{
		Viewport:       Rect{X: 100, Y: 50, W: 240, H: 176},
		Crop:           Rect{X: 132, Y: 50, W: 176, H: 176},
		Battle:         Rect{X: 400, Y: 60, W: 160, H: 220},
		HP:             Rect{X: 20, Y: 300, W: 100, H: 8},
		Mana:           Rect{X: 20, Y: 312, W: 100, H: 8},
		GridCols:       15,
		GridRows:       11,
		BarWidth:       13,
		BarHeight:      4,
		BarBorder:      1,
		DecisionRadius: 4,
		BattleBarWidth: 13, BattleBarHeight: 4, BattleBarBorder: 1,
		BattleRowPitch: 22, BattleFrame: "#ff5050",
	}
}

func TestCombatConfigAcceptsCalibrated(t *testing.T) {
	if err := calibrated().withDefaults().validate(); err != nil {
		t.Fatalf("poprawna kalibracja odrzucona: %v", err)
	}
}

// The black-threshold rule's boundary sits exactly at BlackMax 121 (the
// darkest default colour's largest channel, 133, minus the default Tolerance
// of 12): one below that, 120 must still validate cleanly.
func TestCombatConfigAcceptsBlackMaxAtTheBoundary(t *testing.T) {
	c := calibrated()
	c.BlackMax = 120
	if err := c.withDefaults().validate(); err != nil {
		t.Fatalf("BlackMax 120 powinien przejść walidację: %v", err)
	}
}

func TestCombatConfigBattleToleranceDefaults(t *testing.T) {
	c := calibrated().withDefaults()
	if c.BattleBarTolerance != 80 {
		t.Errorf("domyślna tolerancja battle %d, oczekiwano 80", c.BattleBarTolerance)
	}
	if c.BattleBlackMax != 48 {
		t.Errorf("domyślny próg czerni battle %d, oczekiwano 48", c.BattleBlackMax)
	}
	if c.BattleEdgeTolerance != 1 {
		t.Errorf("domyślna tolerancja brzegu battle %d, oczekiwano 1", c.BattleEdgeTolerance)
	}
	// The game-window edge tolerance stays zero: zero is a legal, meaningful
	// value there, so withDefaults must not promote it.
	if c.BarEdgeTolerance != 0 {
		t.Errorf("tolerancja brzegu okna gry %d, oczekiwano 0", c.BarEdgeTolerance)
	}
}

func TestCombatConfigWideBattleBarAccepted(t *testing.T) {
	c := calibrated()
	c.BattleBarWidth = 262 // real 5K battle bar, wider than the old 256 cap
	if err := c.withDefaults().validate(); err != nil {
		t.Fatalf("pasek battle 262 px odrzucony: %v", err)
	}
}

func TestCombatConfigBattleBlackMaxAbsorbsColour(t *testing.T) {
	c := calibrated()
	// Battle pair must run the same "colour not swallowed" check as the game
	// window: darkest default channel 133 minus battle tolerance must stay
	// above battle black max.
	c.BattleBarTolerance = 80
	c.BattleBlackMax = 120
	if err := c.withDefaults().validate(); err == nil {
		t.Fatal("próg czerni battle pochłaniający barwę musi być odrzucony")
	}
}

func TestCombatConfigUncalibratedIsLegal(t *testing.T) {
	if err := (CombatConfig{}).withDefaults().validate(); err != nil {
		t.Fatalf("brak kalibracji musi być dozwolony: %v", err)
	}
	if (CombatConfig{}).Enabled() {
		t.Error("pusta kalibracja nie może być włączona")
	}
	if !calibrated().Enabled() {
		t.Error("pełna kalibracja musi być włączona")
	}
}

func TestCombatConfigDefaults(t *testing.T) {
	c := CombatConfig{Viewport: Rect{W: 240, H: 176}, Crop: Rect{X: 32, W: 176, H: 176},
		BarWidth: 13, BarHeight: 4, BarBorder: 1,
		BattleBarWidth: 13, BattleBarHeight: 4, BattleBarBorder: 1, BattleRowPitch: 22,
		BattleFrame: "#ff5050"}.withDefaults()
	if c.GridCols != 15 || c.GridRows != 11 {
		t.Errorf("domyślna siatka %dx%d, oczekiwano 15x11", c.GridCols, c.GridRows)
	}
	if c.DecisionRadius != 4 {
		t.Errorf("domyślny promień %v, oczekiwano 4", c.DecisionRadius)
	}
	if c.BlackMax == 0 || c.BarTolerance == 0 || c.BattleFrameCoverage == 0 {
		t.Errorf("progi barw nie dostały wartości domyślnych: %+v", c)
	}
	if len(c.BarColors) == 0 {
		t.Error("lista barw paska nie dostała wartości domyślnych")
	}
}

func TestCombatConfigRecommendedCropIsElevenTiles(t *testing.T) {
	c := calibrated().withDefaults()
	got := c.RecommendedCrop()
	// A radius of 4 reaches 5 tiles in every direction, i.e. 11 columns of 16
	// px. The window only has 11 rows, so vertically the crop is clipped to
	// the full height - which is why the crop only saves a quarter, not a
	// multiple.
	if got.W != 176 || got.H != 176 {
		t.Errorf("zalecany wycinek %dx%d, oczekiwano 176x176", got.W, got.H)
	}
	if got.X != 132 || got.Y != 50 {
		t.Errorf("zalecany wycinek zaczyna się na %d,%d, oczekiwano 132,50", got.X, got.Y)
	}
}

func TestCombatConfigRejections(t *testing.T) {
	tests := []struct {
		name string
		edit func(*CombatConfig)
		want string
	}{
		{"wycinek poza oknem gry", func(c *CombatConfig) { c.Crop.X = 0 }, "wycinek"},
		{"wycinek za mały na promień", func(c *CombatConfig) {
			c.Crop = Rect{X: 164, Y: 50, W: 112, H: 176}
		}, "promień"},
		{"okno gry mniejsze niż siatka", func(c *CombatConfig) { c.Viewport.W = 10 }, "okno gry"},
		{"za dużo kolumn", func(c *CombatConfig) { c.GridCols = 200 }, "siatka"},
		{"obwódka szersza niż pasek", func(c *CombatConfig) { c.BarBorder = 3 }, "obwódka"},
		{"promień poza zakresem", func(c *CombatConfig) { c.DecisionRadius = 99 }, "promień"},
		{"barwa nie do odczytania", func(c *CombatConfig) { c.BarColors = []string{"zielony"} }, "barwa"},
		{"barwa ramki nie do odczytania", func(c *CombatConfig) { c.BattleFrame = "xyz" }, "ramki"},
		{"odstęp wierszy mniejszy niż pasek", func(c *CombatConfig) { c.BattleRowPitch = 2 }, "odstęp"},
		{"pasek HP bez szerokości", func(c *CombatConfig) { c.HP = Rect{X: 20, Y: 300, W: 2, H: 8} }, "HP"},
		// The default colours' darkest is #850c0c, whose largest channel is
		// 133. With the default Tolerance of 12, a BlackMax of 121 or more
		// makes that colour simultaneously count as fill and as dark, so this
		// must be refused rather than silently misreading every dark-red bar.
		{"próg czerni pochłania barwę paska", func(c *CombatConfig) { c.BlackMax = 121 }, "próg czerni"},
		{"tolerancja brzegu poza zakresem", func(c *CombatConfig) { c.BattleEdgeTolerance = 9 }, "tolerancja brzegu"},
		{"tolerancja battle poza zakresem", func(c *CombatConfig) { c.BattleBarTolerance = 200 }, "tolerancja"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := calibrated()
			tt.edit(&c)
			err := c.withDefaults().validate()
			if err == nil {
				t.Fatalf("kalibracja przeszła, choć nie powinna")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("komunikat %q nie zawiera %q", err.Error(), tt.want)
			}
		})
	}
}

// BarTolerance and BlackMax are only promoted away from zero by withDefaults,
// so this calls validate() directly rather than through the usual
// withDefaults().validate() pipeline: going through withDefaults first would
// silently turn the explicit 0 into 12 before validate() ever saw it, and the
// rejection this test is about would never fire. calibrated() itself never
// sets BarTolerance, so it already carries the zero this checks.
func TestCombatConfigZeroToleranceIsRejectedOnItsOwn(t *testing.T) {
	c := calibrated()
	c.BarTolerance = 0
	err := c.validate()
	if err == nil {
		t.Fatal("zerowa tolerancja przeszła walidację, choć nie powinna")
	}
	if !strings.Contains(err.Error(), "tolerancja") {
		t.Errorf("komunikat %q nie zawiera %q", err.Error(), "tolerancja")
	}
}

// The anchor is derived from the character's own bar, not from configuration.
// That is the only way to avoid guessing pixels.
func TestGridDerivesAnchorFromSelfBar(t *testing.T) {
	c := calibrated().withDefaults()
	c.HasSelfBar, c.SelfBarX, c.SelfBarY = true, 81, 74
	g := c.grid()
	dx, dy := g.Offset(vision.Bar{X: 81, Y: 74})
	if dx != 0 || dy != 0 {
		t.Errorf("własny pasek dał offset %.6f,%.6f, oczekiwano 0,0", dx, dy)
	}
}
