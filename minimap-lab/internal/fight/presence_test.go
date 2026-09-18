package fight_test

import (
	"testing"
	"time"

	"minimap-lab/internal/fight"
)

func TestPresence(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	type observation struct {
		holds bool
		at    time.Duration // offset from base
	}
	tests := []struct {
		name    string
		observe []observation
		confirm time.Duration
		want    bool // wynik ostatniej obserwacji
	}{
		{
			name:    "dwie klatki i wystarczający czas potwierdzają",
			observe: []observation{{true, 0}, {true, 160 * time.Millisecond}},
			confirm: 150 * time.Millisecond,
			want:    true,
		},
		{
			name:    "dwie klatki ale za mało czasu nie potwierdzają",
			observe: []observation{{true, 0}, {true, 100 * time.Millisecond}},
			confirm: 150 * time.Millisecond,
			want:    false,
		},
		{
			name:    "jedna klatka nigdy nie potwierdza, nawet przy zerowym ConfirmMS",
			observe: []observation{{true, 0}},
			confirm: 0,
			want:    false,
		},
		{
			name: "przerwa dłuższa niż 500 ms zeruje ciągłość",
			observe: []observation{
				{true, 0}, {true, 160 * time.Millisecond},
				{true, 700 * time.Millisecond}, {true, 750 * time.Millisecond},
			},
			confirm: 150 * time.Millisecond,
			// Trzecia obserwacja jest 540 ms po drugiej - dalej niż próg
			// 500 ms - więc zaczyna nowy ciąg. Czwarta jest tylko 50 ms po
			// tym restarcie, za mało samodzielnie.
			want: false,
		},
		{
			name: "fałsz w środku zeruje, kolejne prawdziwe klatki liczą od nowa",
			observe: []observation{
				{true, 0}, {true, 160 * time.Millisecond}, {false, 200 * time.Millisecond},
				{true, 250 * time.Millisecond}, {true, 410 * time.Millisecond},
			},
			confirm: 150 * time.Millisecond,
			want:    true,
		},
		{
			name:    "duplikat tej samej klatki nie liczy się jako druga obserwacja",
			observe: []observation{{true, 0}, {true, 0}},
			confirm: 0,
			// Gdyby duplikat (identyczny capturedAt) liczył się jako nowa
			// klatka, ConfirmMS=0 dałoby true już tutaj - druga obserwacja
			// wciąż musi widzieć tylko jedną prawdziwą klatkę.
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p fight.Presence
			var got bool
			for _, o := range tt.observe {
				got = p.Observe(o.holds, base.Add(o.at), tt.confirm)
			}
			if got != tt.want {
				t.Errorf("wynik ostatniej obserwacji = %v, oczekiwano %v", got, tt.want)
			}
		})
	}
}
