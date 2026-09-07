package brain

import (
	"encoding/json"

	"minimap-lab/internal/mapdata"
)

// State is everything the panel is told about the bot. It is answered on every
// frame, so it has to stay small and bounded: no waypoints, no atlas, no full
// history. A thousand-point route riding in each frame response would be
// megabytes a minute for nothing.
type State struct {
	// StateVersion rises whenever anything below changes, and LastFrameSeq
	// says which frame the loop has actually finished with. Together they let
	// the panel tell "my frame has been processed" from "this is the answer to
	// an earlier one" - the handler never waits for the match, so a snapshot
	// does not describe the frame that fetched it.
	StateVersion uint64 `json:"state_version"`
	LastFrameSeq uint64 `json:"last_frame_seq"`
	// LastMatchSeq says which frame the position below answers. The match runs
	// off the loop goroutine, so a snapshot carrying LastFrameSeq for a frame
	// does not yet describe where that frame found the character.
	LastMatchSeq uint64 `json:"last_match_seq"`

	Armed  bool   `json:"armed"`
	Zoom   int    `json:"zoom"`
	Reason string `json:"reason,omitempty"`

	Position *mapdata.Position `json:"position"`
	// PositionAgeMS is measured from the capture of the frame that produced
	// the position, and is the only clock the freshness gate consults.
	PositionAgeMS *int `json:"position_age_ms"`

	Match    MatchState    `json:"match"`
	Route    RouteState    `json:"route"`
	Executor ExecState     `json:"executor"`
	Recorder RecorderState `json:"recorder"`
	Combat   CombatState   `json:"combat"`

	LastAction *ActionState `json:"last_action,omitempty"`
	// PreviewRevision changes when the neighbourhood picture would look
	// different, so the panel knows when refetching the preview is worthwhile.
	PreviewRevision uint64     `json:"preview_revision"`
	Log             []LogEntry `json:"log"`
}

type MatchState struct {
	Found           bool    `json:"found"`
	Mode            string  `json:"mode,omitempty"`
	Score           float64 `json:"score,omitempty"`
	MatchMS         float64 `json:"match_ms,omitempty"`
	Samples         int     `json:"samples,omitempty"`
	SearchPositions int     `json:"search_positions,omitempty"`
	Reason          string  `json:"reason,omitempty"`
	Hz              float64 `json:"hz"`
	Success         float64 `json:"success"`
	RoundTripMS     float64 `json:"round_trip_ms,omitempty"`
	SearchedFloors  []int   `json:"searched_floors,omitempty"`
}

type RouteState struct {
	Loaded    bool   `json:"loaded"`
	Following bool   `json:"following"`
	Name      string `json:"name,omitempty"`
	Index     int    `json:"index"`
	Count     int    `json:"count"`
	Finished  bool   `json:"finished"`
	// Next is the sentence the panel shows: an instruction for a transition,
	// a direction for a step, or why nothing is happening.
	Next    string `json:"next,omitempty"`
	PathLen int    `json:"path_len"`
}

type RecorderState struct {
	Auto  bool `json:"auto"`
	Count int  `json:"count"`
	// Skipped counts positions refused as waypoints because the map calls the
	// tile impassable - each one is a reading that was probably wrong.
	Skipped int `json:"skipped"`
	// Waiting is true while walkability data for the area has not arrived, so
	// the recorder is holding off rather than recording unverified points.
	Waiting bool `json:"waiting"`
}

type ActionState struct {
	Kind      string `json:"kind"`
	Direction string `json:"direction,omitempty"`
	Type      string `json:"type,omitempty"`
	Status    string `json:"status"`
	Key       string `json:"key,omitempty"`
	Reason    string `json:"reason,omitempty"`
	AgeMS     int    `json:"age_ms"`
}

type LogEntry struct {
	Seq  uint64 `json:"seq"`
	Text string `json:"text"`
}

// TileVerdict is what the map data says about one tile. It exists because the
// recorder's gate needs three answers, not two: a tile the pack has no data
// for is not the same as a tile the pack calls a wall, and neither is the same
// as not having loaded the area yet.
type TileVerdict int

const (
	// TileUnknown means walkability data for the area has not been read yet.
	TileUnknown TileVerdict = iota
	TileWalkable
	TileBlocked
	TileNoMapData
)

// marshalState exists so tests can weigh a snapshot the way the wire does.
func marshalState(s *State) ([]byte, error) { return json.Marshal(s) }

// CombatState is what the panel is told about what the bot can see. Scalars
// only: the snapshot is answered on every single frame, so the rectangles
// behind these numbers go out through GET /api/vision instead.
type CombatState struct {
	// Calibrated is false until the game window and the crop are both
	// measured; everything below is then zero.
	Calibrated bool `json:"calibrated"`
	// BarsTotal counts creature bars inside the crop, excluding the
	// character's own and any the map sieve rejected - BarsTotal +
	// RejectedByMap is the raw count vision.Find returned.
	// MonstersInRange counts those within the decision radius, measured as a
	// Chebyshev distance in tiles and rounded to the nearest whole tile (see
	// finishVision), so the effective radius is round(DecisionRadius): a
	// half-integer radius (legal down to 0.5) reaches one ring further than
	// its own number suggests, and at 0.5 the whole 3x3 ring counts.
	BarsTotal       int `json:"bars_total"`
	MonstersInRange int `json:"monsters_in_range"`
	// RejectedByMap counts bars dropped because the map data calls their tile
	// impassable - a creature cannot stand in a wall, so such a bar was drawn
	// from another floor. Always zero while the position is unknown, because
	// the sieve has no tile to ask about.
	RejectedByMap int `json:"rejected_by_map"`
	// MixedCrowd is true when more creature bars sit in the crop than the
	// battle list shows monster rows for the whole screen, which proves
	// something in the crop is not a monster. The test is one-sided and
	// deliberately conservative: false does not mean the crowd is clean.
	MixedCrowd bool `json:"mixed_crowd"`
	BattleRows int  `json:"battle_rows"`
	// BattleTruncated says the list is scrolled, so BattleRows is a floor and
	// MixedCrowd stops meaning anything at all.
	BattleTruncated bool `json:"battle_truncated"`
	// TargetRow is the entry carrying the attack frame, counting from zero.
	// Nil means nothing is being attacked - which is what tells a click on the
	// list from a click that would cancel the attack.
	TargetRow *int `json:"target_row"`

	HPPct   float64 `json:"hp_pct"`
	HPOK    bool    `json:"hp_ok"`
	ManaPct float64 `json:"mana_pct"`
	ManaOK  bool    `json:"mana_ok"`
	// Reason carries why a reading was refused, for the panel to show.
	Reason string `json:"reason,omitempty"`
}

// VisionView is the panel's diagnostic picture of one frame. It never rides in
// the snapshot - dozens of rectangles per frame is exactly the payload the
// snapshot's own comment forbids - so the panel fetches it separately, and
// only while it is showing the preview.
type VisionView struct {
	Have      bool      `json:"have"`
	CropW     int       `json:"crop_w"`
	CropH     int       `json:"crop_h"`
	Bars      []BarView `json:"bars"`
	Battle    []RowView `json:"battle"`
	Truncated bool      `json:"truncated"`
	HP        float64   `json:"hp"`
	HPOK      bool      `json:"hp_ok"`
	Mana      float64   `json:"mana"`
	ManaOK    bool      `json:"mana_ok"`
	Reason    string    `json:"reason,omitempty"`
}

type BarView struct {
	X    int     `json:"x"`
	Y    int     `json:"y"`
	Fill int     `json:"fill"`
	HP   float64 `json:"hp"`
	DX   float64 `json:"dx"`
	DY   float64 `json:"dy"`
	Dist float64 `json:"dist"`
	// InRange is whether this bar counted toward MonstersInRange, decided
	// here rather than left for the panel to re-derive: the preview must
	// never recompute the radius threshold, or the two would drift the
	// moment the rule changes on this side.
	InRange bool `json:"in_range"`
}

type RowView struct {
	X        int     `json:"x"`
	Y        int     `json:"y"`
	HP       float64 `json:"hp"`
	Targeted bool    `json:"targeted"`
}
