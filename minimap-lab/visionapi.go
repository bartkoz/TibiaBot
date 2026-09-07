package main

import "net/http"

// visionView answers the panel's diagnostic picture of the last frame: where
// every bar was, which tile it maps to, what the battle list showed. It is a
// route of its own for the same reason the neighbourhood preview is: dozens of
// rectangles have no business riding in a snapshot answered on every frame.
func (s *server) visionView(w http.ResponseWriter, r *http.Request) {
	if !s.loopReady(w) {
		return
	}
	writeJSON(w, s.loop.VisionSnapshot(r.Context()))
}
