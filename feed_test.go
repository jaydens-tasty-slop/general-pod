package main

import (
	"bytes"
	"encoding/xml"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-fonts/liberation/liberationserifbold"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

func fixtureEpisode() Episode {
	return Episode{
		ID: "42", URI: "/general-conference/2025/04/example", Title: "Faith & Joy <Today>",
		Speaker: "Ada Example", Session: "Saturday Morning Session",
		Publication: time.Date(2025, 4, 5, 0, 0, 0, 0, time.UTC),
		Conference:  Conference{Year: 2025, Month: 4}, Position: 2, EpisodeNumber: 2,
		AudioURL:   "https://assets.churchofjesuschrist.org/audio.mp3?a=1&b=2",
		AudioBytes: 12345, DurationMS: 90500, ArtworkPath: "/assets/eng/episodes/42.jpg",
	}
}

func TestRenderFeedIsExactAndEscaped(t *testing.T) {
	language := Language{ISOCode: "eng", BCP47: "en", NativeName: "English"}
	data := feedDataFor("https://example.test", language, "eng", []Episode{fixtureEpisode()})
	feed, err := renderFeed(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`xmlns:content="http://purl.org/rss/1.0/modules/content/"`,
		`xmlns:podcast="https://podcastindex.org/namespace/1.0"`,
		`<title>Faith &amp; Joy &lt;Today&gt; — Ada Example</title>`,
		`url="https://assets.churchofjesuschrist.org/audio.mp3?a=1&amp;b=2"`,
		`<podcast:season name="195th Annual General Conference">195</podcast:season>`,
		`<itunes:duration>91</itunes:duration>`,
		`Sat, 05 Apr 2025 00:00:00 +0000`,
	} {
		if !bytes.Contains(feed, []byte(expected)) {
			t.Errorf("feed does not contain %q", expected)
		}
	}
	decoder := xml.NewDecoder(bytes.NewReader(feed))
	for {
		if _, err := decoder.Token(); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("generated XML is not well formed: %v", err)
		}
	}
	dataAgain := feedDataFor("https://example.test", language, "eng", []Episode{fixtureEpisode()})
	if data.GUID != dataAgain.GUID || data.Episodes[0].GUID != dataAgain.Episodes[0].GUID {
		t.Fatal("Podcasting 2.0 GUIDs are not deterministic")
	}
}

func TestArtworkDimensionsPalettesAndOverflow(t *testing.T) {
	item := fixtureEpisode()
	item.Title = strings.Repeat("A thoughtfully wrapped title with exceptionally long words Supercalifragilisticexpialidocious ", 5)
	item.Speaker = "A Speaker With A Surprisingly Long Display Name That Must Still Fit"
	parsed, err := opentype.Parse(liberationserifbold.TTF)
	if err != nil {
		t.Fatal(err)
	}
	size, lines := fitLines(parsed, item.Title, scaled(116), scaled(52), scaled(1120), 5)
	if len(lines) > 5 {
		t.Fatalf("overflow fitter returned %d lines", len(lines))
	}
	face := fontFace(parsed, float64(size))
	for _, line := range lines {
		if width := font.MeasureString(face, line).Ceil(); width > scaled(1120) {
			t.Errorf("fitted line is %d pixels wide: %q", width, line)
		}
	}
	face.Close()
	for _, month := range []int{4, 10} {
		item.Conference.Month = month
		filename := filepath.Join(t.TempDir(), "art.jpg")
		if err := renderArtwork(filename, item, "", ""); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(filename)
		if err != nil {
			t.Fatal(err)
		}
		image, err := jpeg.Decode(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if image.Bounds().Dx() != artworkSize || image.Bounds().Dy() != artworkSize {
			t.Fatalf("got %v artwork, want %dx%d", image.Bounds(), artworkSize, artworkSize)
		}
	}
}

func TestLanguageSlug(t *testing.T) {
	if got := languageSlug(Language{ISOCode: "eng", BCP47: "en"}); got != "eng" {
		t.Fatal(got)
	}
	if got := languageSlug(Language{ISOCode: "spa", BCP47: "es"}); got != "spa" {
		t.Fatal(got)
	}
}

func TestConferenceNumbersAndOrdinals(t *testing.T) {
	if got := conferenceNumber(2025); got != 195 {
		t.Fatal(got)
	}
	for number, expected := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 195: "195th"} {
		if got := ordinal(number); got != expected {
			t.Errorf("ordinal(%d) = %q, want %q", number, got, expected)
		}
	}
}
