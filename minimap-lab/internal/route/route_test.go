package route

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

const minimal = `{"version":1,"name":"Trasa","waypoints":[{"x":32958,"y":32077,"z":7}]}`

func mustParse(t *testing.T, text string) Route {
	t.Helper()
	r, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse(%s): %v", text, err)
	}
	return r
}

func TestWaypointWithoutTypeWalks(t *testing.T) {
	r := mustParse(t, minimal)
	if got := r.Waypoints[0].Type; got != "walk" {
		t.Errorf("typ = %q, oczekiwano walk", got)
	}
	if r.Name != "Trasa" {
		t.Errorf("nazwa = %q, oczekiwano Trasa", r.Name)
	}
}

func TestEveryDocumentedActionTypeIsAccepted(t *testing.T) {
	for _, kind := range Types {
		text := fmt.Sprintf(`{"version":1,"waypoints":[{"x":1,"y":2,"z":3,"type":%q}]}`, kind)
		if got := mustParse(t, text).Waypoints[0].Type; got != kind {
			t.Errorf("typ = %q, oczekiwano %q", got, kind)
		}
	}
}

func TestUnknownActionTypeNamesTheOffendingWaypoint(t *testing.T) {
	_, err := Parse([]byte(`{"version":1,"waypoints":[{"x":1,"y":2,"z":7},{"x":1,"y":2,"z":7,"type":"teleport"}]}`))
	if err == nil {
		t.Fatal("nieznany typ został przyjęty")
	}
	// The message has to point at the waypoint the user must fix; "unknown
	// type" alone leaves them hunting through a thousand-point file.
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("komunikat nie wskazuje winowajcy: %v", err)
	}
}

func TestCoordinatesOutsideTheMapAreRejected(t *testing.T) {
	for _, bad := range []string{
		`{"x":-1,"y":0,"z":7}`,
		`{"x":70000,"y":0,"z":7}`,
		`{"x":0,"y":0,"z":16}`,
		`{"x":0.5,"y":0,"z":7}`,
	} {
		text := fmt.Sprintf(`{"version":1,"waypoints":[%s]}`, bad)
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("przyjęto %s", bad)
		}
	}
}

func TestFutureVersionIsRefusedRatherThanGuessedAt(t *testing.T) {
	_, err := Parse([]byte(`{"version":2,"waypoints":[]}`))
	if err == nil {
		t.Fatal("przyjęto plik z przyszłej wersji")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "wersj") {
		t.Errorf("komunikat nie mówi o wersji: %v", err)
	}
}

func TestEmptyRouteIsValidAndAThousandAndOneIsNot(t *testing.T) {
	if got := len(mustParse(t, `{"version":1,"waypoints":[]}`).Waypoints); got != 0 {
		t.Errorf("pusta trasa ma %d punktów", got)
	}
	many := make([]string, 1001)
	for i := range many {
		many[i] = `{"x":1,"y":2,"z":7}`
	}
	text := fmt.Sprintf(`{"version":1,"waypoints":[%s]}`, strings.Join(many, ","))
	err := func() error { _, err := Parse([]byte(text)); return err }()
	if err == nil {
		t.Fatal("przyjęto trasę ponad limit")
	}
	if !strings.Contains(err.Error(), "1000") {
		t.Errorf("komunikat nie podaje limitu: %v", err)
	}
}

func TestLabelsAreKeptButCapped(t *testing.T) {
	long := strings.Repeat("x", 200)
	text := fmt.Sprintf(`{"version":1,"waypoints":[{"x":1,"y":2,"z":7,"label":%q}]}`, long)
	if got := utf8.RuneCountInString(mustParse(t, text).Waypoints[0].Label); got != MaxLabel {
		t.Errorf("długość etykiety = %d, oczekiwano %d", got, MaxLabel)
	}
}

// Cutting by runes rather than bytes: a Polish label sliced mid-character
// would produce invalid UTF-8, which the JavaScript original could not do.
func TestLabelIsCutOnRuneBoundaries(t *testing.T) {
	long := strings.Repeat("ą", 200)
	text := fmt.Sprintf(`{"version":1,"waypoints":[{"x":1,"y":2,"z":7,"label":%q}]}`, long)
	label := mustParse(t, text).Waypoints[0].Label
	if utf8.RuneCountInString(label) != MaxLabel {
		t.Errorf("długość etykiety = %d run, oczekiwano %d", utf8.RuneCountInString(label), MaxLabel)
	}
	if !utf8.ValidString(label) {
		t.Error("etykieta ucięta w połowie znaku")
	}
}

func TestMalformedInputFailsWithAMessageNotACrash(t *testing.T) {
	for _, text := range []string{``, `{`, `null`, `[]`, `{"version":1}`, `{"version":1,"waypoints":{}}`} {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("przyjęto %q", text)
		}
	}
}

func TestRouteSurvivesRoundTrip(t *testing.T) {
	original := `{"version":1,"name":"Venore","waypoints":[
		{"x":32958,"y":32077,"z":7,"type":"rope","label":"lina"},
		{"x":32958,"y":32077,"z":6,"type":"walk","label":""}]}`
	first := mustParse(t, original)
	data, err := Serialize(first)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	second := mustParse(t, string(data))
	want, _ := json.Marshal(first)
	got, _ := json.Marshal(second)
	if string(want) != string(got) {
		t.Errorf("round trip zmienił trasę:\n%s\n%s", want, got)
	}
}
