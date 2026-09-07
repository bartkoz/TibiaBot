package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVisionRouteRefusedWithoutControl(t *testing.T) {
	s, _ := venoreServer(t)
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://localhost/api/vision", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("bez sterowania /api/vision odpowiedziało %d, oczekiwano 503", rec.Code)
	}
}
