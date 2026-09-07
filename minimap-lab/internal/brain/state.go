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
