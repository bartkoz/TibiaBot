package brain

// This file owns the loop's vision pipeline: reading the client's own bars,
// finding creature bars in the game window, and turning both into the
// CombatState the panel is told about and the VisionView it can ask for
// separately. handleFrame calls into it exactly twice per frame -
// observeVision as soon as a frame arrives, finishVision once via the
// deferred call at the end - and nothing outside this file touches l.combat,
// l.view, l.bars or l.visionGrid directly.

import (
	"context"
	"math"

	"minimap-lab/internal/battle"
	"minimap-lab/internal/frame"
	"minimap-lab/internal/mapdata"
	"minimap-lab/internal/vision"
	"minimap-lab/internal/vitals"
)

// maxVisionBars bounds the diagnostic payload. A crop calibrated onto the
// wrong part of the screen can match hundreds of things; the panel needs to
// see that it went wrong, not to receive all of it.
const maxVisionBars = 64

// observeVision reads the client's own panels and finds the creature bars. It
// is pure with respect to the world: nothing here consults the position.
func (l *Loop) observeVision(f frame.Frame) {
	l.combat, l.view, l.bars = CombatState{}, VisionView{}, nil
	l.hpReading, l.manaReading = vitals.Reading{}, vitals.Reading{}
	// battleRead is the difference between "the list is empty" and "the list
	// never arrived", which the activity machine must not confuse: rows of
	// zero from a frame that never carried the region would drive an exit
	// from a fight that is still going on.
	l.battleRead = false
	cc := l.cfg.Combat
	if !cc.Enabled() {
		return
	}
	l.combat.Calibrated = true
	// The full readings are kept, not just the percentages: the healing rules
	// need to tell "the bar says zero" from "the bar could not be read", and
	// the reason text is what the panel shows when a rule is blocked.
	l.hpReading = vitals.Reading{Reason: "brak regionu paska HP w klatce"}
	if im, ok := f.Image(frame.RegionHP); ok {
		l.hpReading = vitals.Read(im, cc.vitalsOptions())
		l.combat.HPPct, l.combat.HPOK = l.hpReading.Percent, l.hpReading.OK
		l.view.HP, l.view.HPOK = l.hpReading.Percent, l.hpReading.OK
		if !l.hpReading.OK {
			l.combat.Reason = l.hpReading.Reason
		}
	}
	l.manaReading = vitals.Reading{Reason: "brak regionu paska many w klatce"}
	if im, ok := f.Image(frame.RegionMana); ok {
		l.manaReading = vitals.Read(im, cc.vitalsOptions())
		l.combat.ManaPct, l.combat.ManaOK = l.manaReading.Percent, l.manaReading.OK
		l.view.Mana, l.view.ManaOK = l.manaReading.Percent, l.manaReading.OK
		if !l.manaReading.OK && l.combat.Reason == "" {
			l.combat.Reason = l.manaReading.Reason
		}
	}
	if im, ok := f.Image(frame.RegionBattle); ok {
		l.battleRead = true
		o, err := cc.battleOptions()
		if err != nil {
			// Unreachable on a config that passed validate() - it already
			// parses every colour - but guarded the same as Mana's, so an
			// earlier failure is never clobbered by a later one.
			if l.combat.Reason == "" {
				l.combat.Reason = err.Error()
			}
		} else {
			list := battle.Read(im, o)
			l.combat.BattleRows = len(list.Rows)
			l.combat.BattleTruncated, l.view.Truncated = list.Truncated, list.Truncated
			for i, row := range list.Rows {
				if row.Targeted && l.combat.TargetRow == nil {
					idx := i
					l.combat.TargetRow = &idx
				}
				l.view.Battle = append(l.view.Battle, RowView{
					X: row.Bar.X, Y: row.Bar.Y, HP: row.HP, Targeted: row.Targeted,
				})
			}
		}
	}
	im, ok := f.Image(frame.RegionViewport)
	if !ok {
		return
	}
	o, err := cc.barOptions()
	if err != nil {
		// Same guard as above - unreachable on a validated config, but reads
		// like the other two error sites rather than clobbering unconditionally.
		if l.combat.Reason == "" {
			l.combat.Reason = err.Error()
		}
		return
	}
	l.visionGrid = cc.grid()
	l.bars = vision.Find(im, o)
	l.view.Have = true
	l.view.CropW, l.view.CropH = im.Bounds().Dx(), im.Bounds().Dy()
}

// finishVision turns the raw bars into counts. It is separate from
// observeVision because it needs the position, and it recomputes from the raw
// bars rather than accumulating, so running it twice on one frame - which a
// repeated video frame does - cannot double any count.
func (l *Loop) finishVision() {
	if !l.combat.Calibrated {
		return
	}
	cc := l.cfg.Combat
	l.combat.BarsTotal, l.combat.MonstersInRange, l.combat.RejectedByMap = 0, 0, 0
	l.view.Bars = nil
	for _, b := range l.bars {
		dx, dy := l.visionGrid.Offset(b)
		if l.blockedTile(dx, dy) {
			l.combat.RejectedByMap++
			continue
		}
		dist := vision.Distance(dx, dy)
		l.combat.BarsTotal++
		// Chebyshev(fractional) <= R+0.5 is rounding to the nearest whole
		// tile, expressed as an inequality: the threshold sits at the
		// midpoint of a walk, maximally far from every resting value, so a
		// creature standing still at distance R cannot flicker in and out of
		// the count when detection jitters by a pixel (1/16 of a tile at
		// this calibration). It errs toward counting rather than missing,
		// and it means the effective radius is round(DecisionRadius): a
		// half-integer radius (legal down to 0.5) reaches one ring further
		// than its own number suggests - at 0.5 the threshold is 1.0, so the
		// whole 3x3 ring around the character counts.
		//
		// Computed once and reused for both the counter and BarView.InRange,
		// so the panel's preview can colour by the same answer instead of
		// re-deriving this threshold in JS.
		inRange := dist <= cc.DecisionRadius+0.5
		if inRange {
			l.combat.MonstersInRange++
		}
		if len(l.view.Bars) < maxVisionBars {
			l.view.Bars = append(l.view.Bars, BarView{
				X: b.X, Y: b.Y, Fill: b.Fill, HP: b.HP(cc.geometry()),
				DX: dx, DY: dy, Dist: dist, InRange: inRange,
			})
		}
	}
	// One-sided on purpose: more bars than rows proves a non-monster is in the
	// crop, but equal or fewer proves nothing, because the rows cover the whole
	// screen while the bars cover only the crop.
	l.combat.MixedCrowd = !l.combat.BattleTruncated &&
		l.combat.BattleRows > 0 && l.combat.BarsTotal > l.combat.BattleRows
}

// blockedTile is the cheap sieve against creatures the client drew from
// another floor: a creature cannot stand in a wall. It does nothing while the
// position is unknown, which costs nothing - counting never needed it.
func (l *Loop) blockedTile(dx, dy float64) bool {
	if l.position == nil {
		return false
	}
	p := *l.position
	return l.deps.Tile(mapdata.Position{
		X: p.X + int(math.Round(dx)), Y: p.Y + int(math.Round(dy)), Z: p.Z,
	}) == TileBlocked
}

// VisionSnapshot hands the panel the diagnostic picture of the last frame.
func (l *Loop) VisionSnapshot(ctx context.Context) VisionView {
	var out VisionView
	l.do(ctx, func() {
		out = l.view
		out.Bars = append([]BarView(nil), l.view.Bars...)
		out.Battle = append([]RowView(nil), l.view.Battle...)
	})
	return out
}
