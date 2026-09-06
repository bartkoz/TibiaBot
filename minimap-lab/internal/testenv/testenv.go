// Package testenv resolves the paths shared by tests in every package.
//
// It exists because `go test` runs each package in its own directory, so a
// path written relative to the working directory means something different
// depending on which package asked. Counting "../" levels per package is the
// kind of detail that silently rots the moment a file moves - and these
// fixtures back tests that skip themselves when the data is missing, so a
// wrong path would look like a pass, not a failure.
//
// Only _test.go files import this package, so the "testing" dependency never
// reaches the binary.
package testenv

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// mapPackEnv gates the tests that need the downloaded minimap pack, which is
// far too large to keep in the repository.
const mapPackEnv = "MINIMAP_REAL_MAP_TEST"

// RepoRoot walks up from the test's working directory to the directory holding
// go.mod. Every other path here is derived from it.
func RepoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("nie znaleziono go.mod powyżej %s", dir)
		}
		dir = parent
	}
}

// MapDir points at the downloaded minimap pack next to the repository, and
// skips the test unless MINIMAP_REAL_MAP_TEST=1 asks for it.
func MapDir(t testing.TB) string {
	t.Helper()
	if os.Getenv(mapPackEnv) != "1" {
		t.Skipf("ustaw %s=1, aby testować na mapach pobranych obok repozytorium", mapPackEnv)
	}
	return filepath.Join(RepoRoot(t), "..", "data", "minimap")
}

// FixturePath names a file in the repository-root testdata directory.
func FixturePath(t testing.TB, name string) string {
	t.Helper()
	return filepath.Join(RepoRoot(t), "testdata", name)
}

// LoadFixture decodes a PNG from testdata.
func LoadFixture(t testing.TB, name string) image.Image {
	t.Helper()
	f, err := os.Open(FixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return im
}

// FixtureBytes reads a testdata file whole, for tests that post it as an upload.
func FixtureBytes(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(FixturePath(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// SavePNG writes an image out, for tests that keep a diagnostic artefact.
func SavePNG(t testing.TB, path string, im image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, im)
	closeErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

// Calibration carries the panel settings a fixture was captured with. It is a
// plain struct rather than a locate.Options on purpose: internal/locate's own
// tests import this package, so naming a locate type here would close an
// import cycle. Each caller copies the fields into whatever it needs.
type Calibration struct {
	Zoom       int
	MarkerX    int
	MarkerY    int
	MaskRadius int
	MinScore   float64
	MinGap     float64
}

// VenoreCalibration reads the saved Venore capture correctly. Two packages
// build the same fixture from these numbers - internal/locate's tracking test
// and the root package's HTTP tracking test - and that pair only works as a
// cross-check for as long as both use the same ones, so they are kept here
// rather than written out twice.
func VenoreCalibration() Calibration {
	return Calibration{Zoom: 1, MarkerX: 52, MarkerY: 57, MaskRadius: 5, MinScore: .85, MinGap: .015}
}
