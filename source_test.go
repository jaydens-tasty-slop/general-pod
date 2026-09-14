package main

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestReadPackageUsesDirectAppAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE subitem (id TEXT, uri TEXT, position INTEGER, title TEXT, version INTEGER, type TEXT, publicationDate TEXT)`,
		`CREATE TABLE nav_item (subitem_id TEXT, subtitle TEXT)`,
		`CREATE TABLE related_audio_item (subitem_id TEXT, media_url TEXT, file_size INTEGER, duration INTEGER)`,
		`CREATE TABLE subitem_content (subitem_id TEXT, content_html TEXT)`,
		`INSERT INTO subitem VALUES ('session', '/session', 0, 'Saturday Morning Session', 1, 'conferenceSession', '2025-04-05T00:00:00.000')`,
		`INSERT INTO subitem VALUES ('talk', '/general-conference/2025/04/talk', 1, 'A Talk', 1, 'default', '2025-04-05T00:00:00.000')`,
		`INSERT INTO nav_item VALUES ('talk', 'A Speaker')`,
		`INSERT INTO related_audio_item VALUES ('talk', 'https://assets.churchofjesuschrist.org/talk.mp3', 7654, 321000)`,
		`INSERT INTO subitem_content VALUES ('talk', '<header><p class="author-role">Of the Quorum of the Twelve Apostles</p></header>')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	items, err := readPackage(path, Conference{Year: 2025, Month: 4}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items", len(items))
	}
	item := items[0]
	if item.AudioURL != "https://assets.churchofjesuschrist.org/talk.mp3" || item.AudioBytes != 7654 || item.DurationMS != 321000 {
		t.Fatalf("unexpected app audio metadata: %+v", item)
	}
	if item.Session != "Saturday Morning Session" || item.Speaker != "A Speaker" {
		t.Fatalf("unexpected labels: %+v", item)
	}
	if item.SpeakerTitle != "Of the Quorum of the Twelve Apostles" {
		t.Fatalf("unexpected speaker title: %q", item.SpeakerTitle)
	}
}

func TestExtractAuthorRoleDoesNotCopyTalkBody(t *testing.T) {
	fragment := `<p class="author-role" data-aid="1">Of the Quorum&nbsp;of the Twelve Apostles</p><p>Copyrighted talk body</p>`
	if got := extractAuthorRole(fragment); got != "Of the Quorum of the Twelve Apostles" {
		t.Fatalf("got %q", got)
	}
}

func TestGetJSONRetriesTransientFailures(t *testing.T) {
	attempts := 0
	httpClient := &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts < 3 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("try again")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(`{"catalogVersion":42}`)),
		}, nil
	})}
	client := &churchClient{http: httpClient, cacheDir: t.TempDir()}
	var value struct {
		CatalogVersion int `json:"catalogVersion"`
	}
	if err := client.getJSON(context.Background(), "https://example.test/catalog.json", &value); err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || value.CatalogVersion != 42 {
		t.Fatalf("attempts=%d value=%+v", attempts, value)
	}
}
