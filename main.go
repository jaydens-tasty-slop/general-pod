package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultBaseURL = "https://generalpod.jayd.ml"

type options struct {
	baseURL         string
	cacheDir        string
	outputDir       string
	languages       string
	maxConferences  int
	includeSessions bool
	websiteFallback bool
	generateArtwork bool
	listLanguages   bool
	regularFont     string
	boldFont        string
}

func main() {
	var opts options
	flag.StringVar(&opts.baseURL, "base-url", defaultBaseURL, "public URL of the generated site")
	flag.StringVar(&opts.cacheDir, "cache", ".cache/generalpod", "download cache")
	flag.StringVar(&opts.outputDir, "output", "docs", "site output directory")
	flag.StringVar(&opts.languages, "languages", "eng", "comma-separated ISO 639-3/BCP-47 codes, or all")
	flag.IntVar(&opts.maxConferences, "max-conferences", 0, "limit conferences per language (0 means all)")
	flag.BoolVar(&opts.includeSessions, "include-sessions", false, "also publish complete conference-session recordings")
	flag.BoolVar(&opts.websiteFallback, "website-fallback", true, "use the Church study API when an app package lacks audio metadata")
	flag.BoolVar(&opts.generateArtwork, "artwork", true, "generate square episode artwork")
	flag.BoolVar(&opts.listLanguages, "list-languages", false, "print Gospel Library languages and exit")
	flag.StringVar(&opts.regularFont, "font", "", "optional TrueType/OpenType font for episode artwork")
	flag.StringVar(&opts.boldFont, "bold-font", "", "optional bold TrueType/OpenType font for episode artwork")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	client := newChurchClient(opts.cacheDir)

	languages, err := client.languages(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if opts.listLanguages {
		for _, language := range languages {
			fmt.Printf("%-12s %-12s %s\n", language.ISOCode, language.BCP47, language.NativeName)
		}
		return
	}

	selected, err := selectLanguages(languages, opts.languages)
	if err != nil {
		log.Fatal(err)
	}
	for _, language := range selected {
		log.Printf("building %s (%s)", language.NativeName, language.ISOCode)
		episodes, err := client.episodes(ctx, language, opts.maxConferences, opts.includeSessions, opts.websiteFallback)
		if err != nil {
			log.Fatal(err)
		}
		if len(episodes) == 0 {
			log.Printf("skipping %s: no conference audio", language.ISOCode)
			continue
		}

		slug := languageSlug(language)
		for i := range episodes {
			episodes[i].ArtworkPath = fmt.Sprintf("/assets/%s/episodes/%s.jpg", slug, episodes[i].ID)
		}
		if opts.generateArtwork {
			artDir := filepath.Join(opts.outputDir, "assets", slug, "episodes")
			if err := os.MkdirAll(artDir, 0o755); err != nil {
				log.Fatal(err)
			}
			if err := renderArtworkSet(artDir, episodes, opts.regularFont, opts.boldFont); err != nil {
				log.Fatal(err)
			}
		}

		feed, err := renderFeed(feedDataFor(strings.TrimRight(opts.baseURL, "/"), language, slug, episodes))
		if err != nil {
			log.Fatal(err)
		}
		feedDir := filepath.Join(opts.outputDir, "podcast", slug)
		if err := os.MkdirAll(feedDir, 0o755); err != nil {
			log.Fatal(err)
		}
		if err := atomicWrite(filepath.Join(feedDir, "feed.xml"), feed, 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %d episodes to %s", len(episodes), filepath.Join(feedDir, "feed.xml"))
	}
}

func selectLanguages(all []Language, requested string) ([]Language, error) {
	if strings.EqualFold(strings.TrimSpace(requested), "all") {
		return all, nil
	}
	wanted := map[string]bool{}
	for _, code := range strings.Split(requested, ",") {
		code = strings.ToLower(strings.TrimSpace(code))
		if code != "" {
			wanted[code] = true
		}
	}
	var selected []Language
	for _, language := range all {
		if wanted[strings.ToLower(language.ISOCode)] || wanted[strings.ToLower(language.BCP47)] {
			selected = append(selected, language)
			delete(wanted, strings.ToLower(language.ISOCode))
			delete(wanted, strings.ToLower(language.BCP47))
		}
	}
	if len(wanted) != 0 {
		unknown := make([]string, 0, len(wanted))
		for code := range wanted {
			unknown = append(unknown, code)
		}
		return nil, fmt.Errorf("unknown language code(s): %s (use -list-languages)", strings.Join(unknown, ", "))
	}
	return selected, nil
}

func languageSlug(language Language) string {
	return strings.ToLower(language.ISOCode)
}
