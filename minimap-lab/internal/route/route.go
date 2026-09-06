// Package route is the route file format, shared by the brain and the panel. A
// route is a list of waypoints; the type says what to do there, and every
// unknown field is dropped so a hand-edited file cannot smuggle anything in.
//
// The messages below break Go's lowercase-error convention on purpose: they
// are shown to the user verbatim in the panel, and they are the exact strings
// the previous JavaScript parser produced. Someone who has seen "Waypoint 3:
// Z musi być liczbą całkowitą 0-15." once should keep seeing it.
package route

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	Version      = 1
	MaxWaypoints = 1000
	MaxLabel     = 64
)

// Types are the transitions a waypoint can carry. "walk" is ordinary ground.
var Types = []string{"walk", "rope", "ladder", "stairs", "hole", "shovel"}

type Waypoint struct {
	X     int    `json:"x"`
	Y     int    `json:"y"`
	Z     int    `json:"z"`
	Type  string `json:"type"`
	Label string `json:"label"`
}

type Route struct {
	Version   int        `json:"version"`
	Name      string     `json:"name"`
	Waypoints []Waypoint `json:"waypoints"`
}

// rawWaypoint reads coordinates as float64 rather than int so that 0.5 fails
// the integrality check with a message about coordinates, instead of falling
// out of the JSON decoder as an unmarshalling error the user cannot act on.
// Every field is a pointer so a missing one is distinguishable from a zero: an
// absent type defaults to walk, while an absent x is an error, not tile zero.
type rawWaypoint struct {
	X     *float64 `json:"x"`
	Y     *float64 `json:"y"`
	Z     *float64 `json:"z"`
	Type  *string  `json:"type"`
	Label *string  `json:"label"`
}

type rawRoute struct {
	Version   *float64         `json:"version"`
	Name      *string          `json:"name"`
	Waypoints *json.RawMessage `json:"waypoints"`
}

func validType(t string) bool {
	for _, known := range Types {
		if known == t {
			return true
		}
	}
	return false
}

// tileValue accepts only whole numbers inside the map, matching the panel's
// Number.isInteger check rather than silently truncating a fractional tile.
func tileValue(v *float64, lo, hi int) (int, bool) {
	if v == nil || *v != math.Trunc(*v) || math.IsInf(*v, 0) {
		return 0, false
	}
	n := int(*v)
	return n, n >= lo && n <= hi
}

func truncate(s string, runes int) string {
	out := []rune(s)
	if len(out) <= runes {
		return s
	}
	return string(out[:runes])
}

func parseWaypoint(raw rawWaypoint, index int) (Waypoint, error) {
	at := fmt.Sprintf("Waypoint %d", index+1)
	x, okX := tileValue(raw.X, 0, 65535)
	y, okY := tileValue(raw.Y, 0, 65535)
	if !okX || !okY {
		return Waypoint{}, fmt.Errorf("%s: X i Y muszą być liczbami całkowitymi 0–65535.", at)
	}
	z, okZ := tileValue(raw.Z, 0, 15)
	if !okZ {
		return Waypoint{}, fmt.Errorf("%s: Z musi być liczbą całkowitą 0–15.", at)
	}
	kind := "walk"
	if raw.Type != nil {
		kind = *raw.Type
	}
	if !validType(kind) {
		return Waypoint{}, fmt.Errorf("%s: nieznany typ „%s”. Dozwolone: %s.", at, kind, strings.Join(Types, ", "))
	}
	label := ""
	if raw.Label != nil {
		label = truncate(*raw.Label, MaxLabel)
	}
	return Waypoint{X: x, Y: y, Z: z, Type: kind, Label: label}, nil
}

func Parse(data []byte) (Route, error) {
	// Probing for an object first separates "this is not JSON at all" from
	// "this is JSON, but a list where an object belongs" - two different
	// mistakes that deserve two different messages.
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return Route{}, fmt.Errorf("Plik nie jest poprawnym JSON-em: %v", err)
	}
	if _, ok := probe.(map[string]any); !ok {
		return Route{}, errors.New("Plik trasy musi być obiektem JSON.")
	}
	var raw rawRoute
	if err := json.Unmarshal(data, &raw); err != nil {
		return Route{}, fmt.Errorf("Plik nie jest poprawnym JSON-em: %v", err)
	}
	version, ok := tileValue(raw.Version, 0, math.MaxInt32)
	if !ok || version != Version {
		shown := "brak"
		if raw.Version != nil {
			shown = fmt.Sprintf("%v", *raw.Version)
		}
		return Route{}, fmt.Errorf("Nieobsługiwana wersja pliku: %s. Ten panel czyta wersję %d.", shown, Version)
	}
	if raw.Waypoints == nil {
		return Route{}, errors.New("Pole waypoints musi być listą.")
	}
	var rawPoints []rawWaypoint
	if err := json.Unmarshal(*raw.Waypoints, &rawPoints); err != nil {
		return Route{}, errors.New("Pole waypoints musi być listą.")
	}
	if len(rawPoints) > MaxWaypoints {
		return Route{}, fmt.Errorf("Trasa ma %d punktów; limit to %d.", len(rawPoints), MaxWaypoints)
	}
	name := ""
	if raw.Name != nil {
		name = truncate(*raw.Name, MaxLabel)
	}
	out := Route{Version: Version, Name: name, Waypoints: make([]Waypoint, 0, len(rawPoints))}
	for i, rp := range rawPoints {
		wp, err := parseWaypoint(rp, i)
		if err != nil {
			return Route{}, err
		}
		out.Waypoints = append(out.Waypoints, wp)
	}
	return out, nil
}

func Serialize(r Route) ([]byte, error) {
	// Waypoints is never nil on the wire: a null there would fail this very
	// parser on the way back in.
	if r.Waypoints == nil {
		r.Waypoints = []Waypoint{}
	}
	r.Version = Version
	return json.MarshalIndent(r, "", "  ")
}
