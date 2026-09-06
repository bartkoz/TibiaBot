package frame

import (
	"encoding/binary"
	"testing"
)

// build assembles a body the way the panel does, so the tests exercise the
// real layout rather than a parser-shaped fiction.
func build(session, seq, videoUS uint64, ageMS uint32, regions []Region) []byte {
	body := make([]byte, HeaderSize+RegionHeader*len(regions))
	copy(body[0:4], Magic)
	body[4] = FormatVersion
	body[5] = byte(len(regions))
	binary.LittleEndian.PutUint64(body[8:], session)
	binary.LittleEndian.PutUint64(body[16:], seq)
	binary.LittleEndian.PutUint64(body[24:], videoUS)
	binary.LittleEndian.PutUint32(body[32:], ageMS)
	for i, r := range regions {
		h := body[HeaderSize+RegionHeader*i:]
		h[0] = byte(r.ID)
		h[1] = 0
		binary.LittleEndian.PutUint16(h[4:], uint16(r.W))
		binary.LittleEndian.PutUint16(h[6:], uint16(r.H))
		binary.LittleEndian.PutUint32(h[8:], uint32(len(r.Pix)))
	}
	for _, r := range regions {
		body = append(body, r.Pix...)
	}
	return body
}

func pixels(w, h int) []byte { return make([]byte, w*h*4) }

func TestParseReadsHeaderAndRegions(t *testing.T) {
	body := build(7, 42, 1_500_000, 30, []Region{{ID: RegionMinimap, W: 2, H: 2, Pix: pixels(2, 2)}})
	f, err := Parse(body)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Session != 7 || f.Seq != 42 || f.VideoTimeUS != 1_500_000 || f.AgeMS != 30 {
		t.Errorf("nagłówek = %+v", f)
	}
	r, ok := f.Regions[RegionMinimap]
	if !ok || r.W != 2 || r.H != 2 || len(r.Pix) != 16 {
		t.Errorf("region minimapy = %+v ok=%v", r, ok)
	}
}

func TestParseRefusesWrongMagic(t *testing.T) {
	body := build(1, 1, 0, 0, nil)
	body[0] = 'X'
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto obcy magic")
	}
}

func TestParseRefusesUnknownFormatVersion(t *testing.T) {
	body := build(1, 1, 0, 0, nil)
	body[4] = FormatVersion + 1
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto nieznaną wersję formatu")
	}
}

// A length that does not match w*h*4 is the shape a truncated or mismatched
// upload takes. Trusting it hands the matcher a buffer it reads past.
func TestParseRefusesRegionLengthThatDoesNotMatchDimensions(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 4, H: 4, Pix: pixels(4, 4)}})
	binary.LittleEndian.PutUint32(body[HeaderSize+8:], 8)
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto region o długości niezgodnej z wymiarami")
	}
}

func TestParseRefusesTruncatedPayload(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 4, H: 4, Pix: pixels(4, 4)}})
	if _, err := Parse(body[:len(body)-4]); err == nil {
		t.Fatal("przyjęto ucięte ciało")
	}
}

func TestParseRefusesTrailingBytes(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 2, H: 2, Pix: pixels(2, 2)}})
	if _, err := Parse(append(body, 0)); err == nil {
		t.Fatal("przyjęto ciało z nadmiarowym ogonem")
	}
}

func TestParseRefusesDuplicateRegion(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{
		{ID: RegionMinimap, W: 1, H: 1, Pix: pixels(1, 1)},
		{ID: RegionMinimap, W: 1, H: 1, Pix: pixels(1, 1)},
	})
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto powtórzony region")
	}
}

// The panel and the server ship in one binary, so an unknown region id is a
// version mismatch, not an extension to tolerate.
func TestParseRefusesUnknownRegionID(t *testing.T) {
	body := build(1, 1, 0, 0, []Region{{ID: RegionID(99), W: 1, H: 1, Pix: pixels(1, 1)}})
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto nieznany identyfikator regionu")
	}
}

// Reserved means reserved: accepting non-zero flags today means they can never
// be given a meaning tomorrow.
func TestParseRefusesNonZeroFlags(t *testing.T) {
	body := build(1, 1, 0, 0, nil)
	body[6] = 1
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto niezerowe flagi")
	}
}

func TestParseRefusesZeroSideAndOversizedRegion(t *testing.T) {
	for _, c := range []struct {
		name string
		w, h int
	}{
		{"zerowa szerokość", 0, 4},
		{"zerowa wysokość", 4, 0},
		{"bok ponad limit", MaxRegionSide + 1, 1},
	} {
		body := build(1, 1, 0, 0, nil)
		body[5] = 1
		header := make([]byte, RegionHeader)
		header[0] = byte(RegionMinimap)
		binary.LittleEndian.PutUint16(header[4:], uint16(c.w))
		binary.LittleEndian.PutUint16(header[6:], uint16(c.h))
		binary.LittleEndian.PutUint32(header[8:], uint32(c.w*c.h*4))
		body = append(body, header...)
		body = append(body, make([]byte, c.w*c.h*4)...)
		if _, err := Parse(body); err == nil {
			t.Errorf("%s: przyjęto region %dx%d", c.name, c.w, c.h)
		}
	}
}

func TestParseRefusesTooManyRegions(t *testing.T) {
	body := build(1, 1, 0, 0, nil)
	body[5] = MaxRegions + 1
	if _, err := Parse(body); err == nil {
		t.Fatal("przyjęto zbyt wiele regionów")
	}
}

func TestParseRefusesBodyShorterThanHeader(t *testing.T) {
	if _, err := Parse(make([]byte, HeaderSize-1)); err == nil {
		t.Fatal("przyjęto ciało krótsze niż nagłówek")
	}
}

// getImageData hands over non-premultiplied RGBA, which is exactly NRGBA.
// Copying 46 kB on every frame would be free pressure on the collector.
func TestImageWrapsPixelsWithoutCopying(t *testing.T) {
	pix := pixels(2, 2)
	pix[0] = 200
	f, err := Parse(build(1, 1, 0, 0, []Region{{ID: RegionMinimap, W: 2, H: 2, Pix: pix}}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	im, ok := f.Image(RegionMinimap)
	if !ok {
		t.Fatal("brak obrazu dla regionu minimapy")
	}
	if im.Bounds().Dx() != 2 || im.Bounds().Dy() != 2 {
		t.Errorf("wymiary = %v", im.Bounds())
	}
	if im.Pix[0] != 200 {
		t.Errorf("piksel = %d, oczekiwano 200", im.Pix[0])
	}
	im.Pix[0] = 5
	if got := f.Regions[RegionMinimap].Pix[0]; got != 5 {
		t.Errorf("obraz skopiował bufor zamiast go owinąć (piksel = %d)", got)
	}
}

func TestImageReportsMissingRegion(t *testing.T) {
	f, err := Parse(build(1, 1, 0, 0, nil))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := f.Image(RegionHP); ok {
		t.Error("zwrócono obraz dla nieprzesłanego regionu")
	}
}
