package web

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/tibiabot/internal/core"
)

func TestIndex(t *testing.T) {
	cfg := core.DefaultConfig()
	srv := NewServer(&cfg)
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html" {
		t.Errorf("content-type = %q, want text/html", ct)
	}
}

func TestGetConfig(t *testing.T) {
	cfg := core.DefaultConfig()
	srv := NewServer(&cfg)
	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var data map[string]interface{}
	json.NewDecoder(w.Body).Decode(&data)
	if _, ok := data["capture"]; !ok {
		t.Error("missing capture in config")
	}
	if _, ok := data["minimap"]; !ok {
		t.Error("missing minimap in config")
	}
}

func TestBotStatus(t *testing.T) {
	cfg := core.DefaultConfig()
	srv := NewServer(&cfg)
	req := httptest.NewRequest("GET", "/api/bot/status", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var data map[string]interface{}
	json.NewDecoder(w.Body).Decode(&data)
	if data["state"] != "IDLE" {
		t.Errorf("state = %v, want IDLE", data["state"])
	}
}
