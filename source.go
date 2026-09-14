package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ulikunitz/xz"
)

const contentBase = "https://ips-cdn-edge.churchofjesuschrist.org/content/production/v4"

type Language struct {
	ID         int    `json:"id"`
	LDSCode    string `json:"ldsCode"`
	ISOCode    string `json:"iso639_3Code"`
	BCP47      string `json:"bcp47Code"`
	NativeName string `json:"nativeName"`
}

type Conference struct {
	ID          string
	URI         string
	Title       string
	LanguageISO string
	Version     int
	Year        int
	Month       int
}

type Episode struct {
	ID            string
	URI           string
	Title         string
	Speaker       string
	SpeakerTitle  string
	Session       string
	Publication   time.Time
	Conference    Conference
	Position      int
	EpisodeNumber int
	AudioURL      string
	AudioBytes    int64
	DurationMS    int64
	ArtworkPath   string
	IsSession     bool
}

type churchClient struct {
	http     *http.Client
	cacheDir string
}

func newChurchClient(cacheDir string) *churchClient {
	return &churchClient{
		http:     &http.Client{Timeout: 90 * time.Second},
		cacheDir: cacheDir,
	}
}

func (c *churchClient) languages(ctx context.Context) ([]Language, error) {
	var languages []Language
	if err := c.getJSON(ctx, contentBase+"/languages/languages.json", &languages); err != nil {
		return nil, fmt.Errorf("Gospel Library language manifest: %w", err)
	}
	sort.Slice(languages, func(i, j int) bool { return languages[i].NativeName < languages[j].NativeName })
	return languages, nil
}

func (c *churchClient) episodes(ctx context.Context, language Language, limit int, includeSessions, websiteFallback bool) ([]Episode, error) {
	var index struct {
		CatalogVersion int `json:"catalogVersion"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/languages/%s/index.json", contentBase, language.ISOCode), &index); err != nil {
		return nil, fmt.Errorf("catalog index for %s: %w", language.ISOCode, err)
	}
	catalogPath := filepath.Join(c.cacheDir, "catalogs", fmt.Sprintf("%s-%d.sqlite", language.ISOCode, index.CatalogVersion))
	if err := c.fetchXZ(ctx, fmt.Sprintf("%s/languages/%s/catalogs/%d.xz", contentBase, language.ISOCode, index.CatalogVersion), catalogPath); err != nil {
		return nil, fmt.Errorf("catalog for %s: %w", language.ISOCode, err)
	}
	conferences, err := readConferences(catalogPath, limit)
	if err != nil {
		return nil, err
	}

	var episodes []Episode
	for _, conference := range conferences {
		conference.LanguageISO = language.ISOCode
		packagePath := filepath.Join(c.cacheDir, "packages", language.ISOCode, fmt.Sprintf("%s-%d.sqlite", conference.ID, conference.Version))
		packageURL := fmt.Sprintf("%s/languages/%s/item-packages/%s/%d.xz", contentBase, language.ISOCode, conference.ID, conference.Version)
		if err := c.fetchXZ(ctx, packageURL, packagePath); err != nil {
			return nil, fmt.Errorf("conference package %s: %w", conference.URI, err)
		}
		items, err := readPackage(packagePath, conference, includeSessions)
		if err != nil {
			return nil, fmt.Errorf("read conference package %s: %w", conference.URI, err)
		}
		for i := range items {
			if items[i].AudioURL == "" && websiteFallback {
				if err := c.fillFromWebsite(ctx, language, &items[i]); err != nil {
					log.Printf("skipping %s: app package has no audio and website fallback returned %v", items[i].URI, err)
					continue
				}
			}
			if items[i].AudioURL != "" && items[i].AudioBytes > 0 {
				episodes = append(episodes, items[i])
			}
		}
	}
	return episodes, nil
}

func readConferences(path string, limit int) ([]Conference, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT id, uri, title, version
		FROM item
		WHERE is_obsolete = 0 AND uri GLOB '/general-conference/[0-9][0-9][0-9][0-9]/[0-9][0-9]'
		ORDER BY uri DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var conferences []Conference
	for rows.Next() {
		var conference Conference
		if err := rows.Scan(&conference.ID, &conference.URI, &conference.Title, &conference.Version); err != nil {
			return nil, err
		}
		parts := strings.Split(conference.URI, "/")
		if len(parts) != 4 {
			continue
		}
		conference.Year, _ = strconv.Atoi(parts[2])
		conference.Month, _ = strconv.Atoi(parts[3])
		if conference.Year == 0 || (conference.Month != 4 && conference.Month != 10) {
			continue
		}
		conferences = append(conferences, conference)
		if limit > 0 && len(conferences) == limit {
			break
		}
	}
	return conferences, rows.Err()
}

func readPackage(path string, conference Conference, includeSessions bool) ([]Episode, error) {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`
		SELECT s.id, s.uri, s.position, s.title, COALESCE(n.subtitle, ''), s.type,
		       COALESCE(s.publicationDate, ''), COALESCE(a.media_url, ''),
		       COALESCE(a.file_size, 0), COALESCE(a.duration, 0),
		       CASE WHEN INSTR(COALESCE(c.content_html, ''), 'class="author-role"') > 0
		            THEN SUBSTR(c.content_html, INSTR(c.content_html, 'class="author-role"') - 3, 512)
		            ELSE '' END
		FROM subitem s
		LEFT JOIN nav_item n ON n.subitem_id = s.id
		LEFT JOIN related_audio_item a ON a.subitem_id = s.id
		LEFT JOIN subitem_content c ON c.subitem_id = s.id
		ORDER BY s.position ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Episode
	var session string
	episodeNumber := 0
	for rows.Next() {
		var item Episode
		var kind, published, authorRoleHTML string
		if err := rows.Scan(&item.ID, &item.URI, &item.Position, &item.Title, &item.Speaker, &kind, &published, &item.AudioURL, &item.AudioBytes, &item.DurationMS, &authorRoleHTML); err != nil {
			return nil, err
		}
		item.SpeakerTitle = extractAuthorRole(authorRoleHTML)
		item.Conference = conference
		item.Publication = parseChurchTime(published, conference)
		if kind == "conferenceSession" {
			session = item.Title
			if !includeSessions {
				continue
			}
			item.IsSession = true
		} else if kind != "default" {
			continue
		}
		item.Session = session
		episodeNumber++
		item.EpisodeNumber = episodeNumber
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Podcast readers display the first item as newest; packages store program order.
	sort.SliceStable(items, func(i, j int) bool { return items[i].Position > items[j].Position })
	return items, nil
}

var (
	authorRolePattern = regexp.MustCompile(`(?is)<p\s+class="author-role"[^>]*>(.*?)</p>`)
	htmlTagPattern    = regexp.MustCompile(`(?s)<[^>]+>`)
)

func extractAuthorRole(fragment string) string {
	match := authorRolePattern.FindStringSubmatch(fragment)
	if len(match) != 2 {
		return ""
	}
	role := html.UnescapeString(htmlTagPattern.ReplaceAllString(match[1], ""))
	role = strings.ReplaceAll(role, "\u00a0", " ")
	return strings.Join(strings.Fields(role), " ")
}

func parseChurchTime(value string, conference Conference) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.000", time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	day := 1
	return time.Date(conference.Year, time.Month(conference.Month), day, 12, 0, 0, 0, time.UTC)
}

func (c *churchClient) fillFromWebsite(ctx context.Context, language Language, item *Episode) error {
	endpoint := "https://www.churchofjesuschrist.org/study/api/v3/language-pages/type/content?lang=" + language.ISOCode + "&uri=" + item.URI
	var page struct {
		Meta struct {
			Audio []struct {
				MediaURL string `json:"mediaUrl"`
				FileSize int64  `json:"fileSize"`
				Duration int64  `json:"duration"`
			} `json:"audio"`
		} `json:"meta"`
	}
	if err := c.getJSON(ctx, endpoint, &page); err != nil {
		return err
	}
	if len(page.Meta.Audio) == 0 || page.Meta.Audio[0].MediaURL == "" {
		return errors.New("no spoken audio in study API")
	}
	item.AudioURL = page.Meta.Audio[0].MediaURL
	item.AudioBytes = page.Meta.Audio[0].FileSize
	item.DurationMS = page.Meta.Audio[0].Duration
	if item.AudioBytes == 0 {
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, item.AudioURL, nil)
		if err != nil {
			return err
		}
		response, err := c.http.Do(req)
		if err != nil {
			return err
		}
		response.Body.Close()
		if response.StatusCode/100 != 2 {
			return fmt.Errorf("audio HEAD: %s", response.Status)
		}
		item.AudioBytes = response.ContentLength
	}
	return nil
}

func (c *churchClient) getJSON(ctx context.Context, url string, destination any) error {
	response, err := c.get(ctx, url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if err := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(destination); err != nil {
		return err
	}
	return nil
}

func (c *churchClient) fetchXZ(ctx context.Context, url, destination string) error {
	if info, err := os.Stat(destination); err == nil && info.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	response, err := c.get(ctx, url)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	reader, err := xz.NewReader(response.Body)
	if err != nil {
		return fmt.Errorf("open xz stream: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".download-*.sqlite")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err = io.Copy(temporary, reader); err != nil {
		temporary.Close()
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, destination)
}

func (c *churchClient) get(ctx context.Context, url string) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "generalpod/2.0 (+https://generalpod.jayd.ml)")
		response, err := c.http.Do(req)
		if err == nil && response.StatusCode/100 == 2 {
			return response, nil
		}
		if response != nil {
			lastErr = fmt.Errorf("GET %s: %s", url, response.Status)
			response.Body.Close()
			if response.StatusCode < 500 && response.StatusCode != http.StatusTooManyRequests {
				return nil, lastErr
			}
		} else {
			lastErr = err
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return nil, fmt.Errorf("GET %s failed after 3 attempts: %w", url, lastErr)
}

func atomicWrite(destination string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".generate-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, destination)
}
