package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"runtime/debug"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

var podcastGUIDNamespace = uuid.MustParse("ead4c236-bf58-58c6-a2c6-a6b28d128cb6")

type FeedData struct {
	BaseURL, SelfURL, ChannelURL, Title, Description, Language, NativeLanguage, Slug string
	BuildDate, PubDate, Generator, GUID, ChannelArtwork                              string
	Episodes                                                                         []FeedEpisode
}

type FeedEpisode struct {
	Title, DisplayTitle, Speaker, Session, Link, Description, GUID, PubDate string
	AudioURL, ArtworkURL                                                    string
	AudioBytes, DurationSeconds, EpisodeNumber, SeasonNumber                int64
	SeasonName, Language                                                    string
	IsSession                                                               bool
}

func feedDataFor(baseURL string, language Language, slug string, source []Episode) FeedData {
	self := fmt.Sprintf("%s/podcast/%s/feed.xml", baseURL, slug)
	title := "General Conference (Unofficial)"
	if language.ISOCode != "eng" {
		title += " — " + language.NativeName
	}
	data := FeedData{
		BaseURL: baseURL, SelfURL: self, ChannelURL: baseURL,
		Title:       title,
		Description: "An unofficial podcast feed for general conference talks published by The Church of Jesus Christ of Latter-day Saints. This feed is unaffiliated with the Church.",
		Language:    language.BCP47, NativeLanguage: language.NativeName, Slug: slug,
		BuildDate: rssDate(time.Now().UTC()), Generator: generatorName(),
		GUID:           uuid.NewSHA1(podcastGUIDNamespace, []byte(normalizeFeedURL(self))).String(),
		ChannelArtwork: baseURL + "/assets/" + slug + "/itunes_image.jpg",
	}
	for _, item := range source {
		link := "https://www.churchofjesuschrist.org/study" + item.URI + "?lang=" + language.ISOCode
		title := item.Title
		description := item.Title
		if item.Speaker != "" {
			title += " — " + item.Speaker
			description += " by " + item.Speaker
		}
		description += ".\n\nLink: " + link
		seasonNumber := int64(conferenceNumber(item.Conference.Year))
		seasonName := item.Conference.Title
		if language.ISOCode == "eng" {
			seasonName = englishConferenceName(item.Conference)
		}
		artworkURL := ""
		if item.ArtworkPath != "" {
			artworkURL = baseURL + item.ArtworkPath
		}
		data.Episodes = append(data.Episodes, FeedEpisode{
			Title: title, DisplayTitle: item.Title, Speaker: item.Speaker, Session: item.Session,
			Link: link, Description: description,
			GUID:    uuid.NewSHA1(podcastGUIDNamespace, []byte("generalpod:"+language.ISOCode+":"+item.ID)).String(),
			PubDate: rssDate(item.Publication), AudioURL: item.AudioURL, AudioBytes: item.AudioBytes,
			DurationSeconds: (item.DurationMS + 500) / 1000,
			EpisodeNumber:   int64(item.EpisodeNumber), SeasonNumber: seasonNumber, SeasonName: seasonName,
			ArtworkURL: artworkURL, Language: language.BCP47, IsSession: item.IsSession,
		})
	}
	if len(data.Episodes) > 0 {
		data.PubDate = data.Episodes[0].PubDate
	}
	return data
}

func conferenceNumber(year int) int { return year - 1830 }

func englishConferenceName(conference Conference) string {
	kind := "Annual"
	if conference.Month == 10 {
		kind = "Semiannual"
	}
	return fmt.Sprintf("%s %s General Conference", ordinal(conferenceNumber(conference.Year)), kind)
}

func ordinal(number int) string {
	suffix := "th"
	if number%100 < 11 || number%100 > 13 {
		switch number % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", number, suffix)
}

func generatorName() string {
	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				version = setting.Value
				break
			}
		}
	}
	return "generalpod/2.0 (" + version + ")"
}

func normalizeFeedURL(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return strings.TrimRight(value, "/")
}

func rssDate(value time.Time) string { return value.Format("Mon, 02 Jan 2006 15:04:05 -0700") }

func xmlText(value string) string {
	var result bytes.Buffer
	_ = xml.EscapeText(&result, []byte(value))
	return result.String()
}

var feedTemplate = template.Must(template.New("feed").Funcs(template.FuncMap{"x": xmlText}).Parse(`<?xml version="1.0" encoding="UTF-8"?>
<rss xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd" xmlns:podcast="https://podcastindex.org/namespace/1.0" version="2.0" xml:lang="{{x .Language}}">
  <channel>
    <atom:link href="{{x .SelfURL}}" rel="self" type="application/rss+xml"/>
    <title>{{x .Title}}</title>
    <link>{{x .ChannelURL}}</link>
    <description>{{x .Description}}</description>
    <language>{{x .Language}}</language>
    <generator>{{x .Generator}}</generator>
    <docs>https://www.rssboard.org/rss-specification</docs>
    <pubDate>{{x .PubDate}}</pubDate>
    <lastBuildDate>{{x .BuildDate}}</lastBuildDate>
    <ttl>1440</ttl>
    <category>Religion &amp; Spirituality</category>
    <category>Christianity</category>
    <image>
      <url>{{x .ChannelArtwork}}</url>
      <title>{{x .Title}}</title>
      <link>{{x .ChannelURL}}</link>
    </image>
    <itunes:author>GeneralPod</itunes:author>
    <itunes:summary>{{x .Description}}</itunes:summary>
    <itunes:explicit>false</itunes:explicit>
    <itunes:type>episodic</itunes:type>
    <itunes:image href="{{x .ChannelArtwork}}"/>
    <itunes:category text="Religion &amp; Spirituality">
      <itunes:category text="Christianity"/>
    </itunes:category>
    <podcast:guid>{{x .GUID}}</podcast:guid>
    <podcast:medium>podcast</podcast:medium>
    <podcast:locked>yes</podcast:locked>
    <podcast:location geo="geo:40.7725,-111.8925" osm="R6146196">Conference Center</podcast:location>
    <podcast:updateFrequency rrule="FREQ=YEARLY;BYMONTH=4,10">Twice yearly, in April and October</podcast:updateFrequency>
    <podcast:image href="{{x .ChannelArtwork}}" alt="General Conference (Unofficial)" aspect-ratio="1/1" width="3000" height="3000" type="image/jpeg" purpose="artwork"/>
{{range .Episodes}}    <item>
      <title>{{x .Title}}</title>
      <link>{{x .Link}}</link>
      <description>{{x .Description}}</description>
      <guid isPermaLink="false">{{x .GUID}}</guid>
      <pubDate>{{x .PubDate}}</pubDate>
      <enclosure url="{{x .AudioURL}}" length="{{.AudioBytes}}" type="audio/mpeg"/>
      <itunes:title>{{x .DisplayTitle}}</itunes:title>
{{if .Speaker}}      <itunes:author>{{x .Speaker}}</itunes:author>
{{end}}      <itunes:subtitle>{{x .Description}}</itunes:subtitle>
      <itunes:summary>{{x .Description}}</itunes:summary>
{{if .DurationSeconds}}      <itunes:duration>{{.DurationSeconds}}</itunes:duration>
{{end}}
      <itunes:episode>{{.EpisodeNumber}}</itunes:episode>
      <itunes:season>{{.SeasonNumber}}</itunes:season>
      <itunes:episodeType>full</itunes:episodeType>
      <itunes:explicit>false</itunes:explicit>
{{if .ArtworkURL}}      <itunes:image href="{{x .ArtworkURL}}"/>
      <podcast:image href="{{x .ArtworkURL}}" alt="Artwork for {{x .DisplayTitle}}" aspect-ratio="1/1" width="3000" height="3000" type="image/jpeg" purpose="artwork"/>
{{end}}{{if .Speaker}}      <podcast:person role="guest" group="cast">{{x .Speaker}}</podcast:person>
{{end}}      <podcast:season name="{{x .SeasonName}}">{{.SeasonNumber}}</podcast:season>
      <podcast:episode display="{{.EpisodeNumber}}">{{.EpisodeNumber}}</podcast:episode>
      <podcast:transcript url="{{x .Link}}" type="text/html" language="{{x .Language}}"/>
      <podcast:contentLink href="{{x .Link}}">Read this talk on ChurchofJesusChrist.org</podcast:contentLink>
    </item>
{{end}}  </channel>
</rss>
`))

func renderFeed(data FeedData) ([]byte, error) {
	var output bytes.Buffer
	if err := feedTemplate.Execute(&output, data); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
