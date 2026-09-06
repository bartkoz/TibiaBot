package mapdata

// Position is one tile in the game world. It lives here, next to the atlas and
// the cost grid, because both are indexed by these coordinates and because
// every layer above - matching, pathfinding, learned blockages and the HTTP
// panel - speaks in them. Putting it in any one of those layers would force
// the others to import that layer purely for a coordinate triple.
type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
	Z int `json:"z"`
}
