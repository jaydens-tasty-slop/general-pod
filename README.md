# GeneralPod 2.0

GeneralPod builds an unofficial podcast feed from the public general conference audio published by The Church of Jesus Christ of Latter-day Saints. It copies no talk text or audio: each episode contains only the talk title, speaker, Church webpage link, and a hot-linked Church MP3 enclosure.

The generator is intentionally database-free. Gospel Library publishes versioned SQLite catalogs and conference packages; those upstream databases are downloaded into `.cache/generalpod` and treated as a disposable HTTP cache. They provide the archive, localized titles and speakers, direct MP3 URL, exact enclosure byte length, and millisecond duration. The Church study website API is used only when a package has a talk without audio metadata

## Generate feeds

Requirements are Go 1.24+ and a C compiler (for the read-only SQLite driver).

```sh
go run .
go run . -languages spa,fra
go run . -languages all
go run . -list-languages
```

Every output directory uses Gospel Library's lowercase `iso639_3Code` without aliases: English is `docs/podcast/eng/feed.xml`, Spanish is `docs/podcast/spa/feed.xml`, and so on. RSS language elements still use the corresponding BCP-47 code required by podcast clients. Useful options include:

- `-max-conferences 1` for a quick current-conference build
- `-output /tmp/generalpod` for a non-destructive preview
- `-include-sessions` to add complete session recordings as bonus episodes
- `-font` and `-bold-font` to supply a font with glyph coverage beyond the embedded Liberation Serif family
- `-artwork=false` to refresh/test feed XML without rerendering the referenced JPEGs

The scheduled GitHub workflow refreshes the English archive weekly. Catalog and package versions make unchanged upstream data cheap to reuse.

## Data path

The APK exposes the production content base URL and this versioned sequence:

1. `languages/languages.json` lists Gospel Library languages.
2. `languages/{iso639_3}/index.json` identifies the current catalog version.
3. `languages/{iso639_3}/catalogs/{version}.xz` contains a SQLite catalog. General conference collection rows have URIs such as `/general-conference/2026/04`.
4. `languages/{iso639_3}/item-packages/{item-id}/{version}.xz` contains the conference navigation and its `related_audio_item` rows.

The `related_audio_item` row is the important part: it supplies the clean `assets.churchofjesuschrist.org` URL, byte length, and duration without downloading the MP3. The fallback endpoint is `https://www.churchofjesuschrist.org/study/api/v3/language-pages/type/content?lang={iso}&uri={talk-uri}`.

## Feed design

The failed v1 relied on `encoding/xml`, then rewrote namespace output after marshaling. V2 uses a typed model plus `text/template` and one explicit XML-escaping function. That guarantees the literal `atom:`, `itunes:`, and `podcast:` prefixes required by real-world validators while keeping untrusted catalog strings escaped.

The feed includes applicable RSS 2.0, Apple Podcasts, and Podcasting 2.0 metadata: self link, deterministic channel and item GUIDs, enclosure metadata, seasons and episode numbers, people, location, update frequency, transcripts/content links, locked/medium declarations, and both legacy and modern artwork tags. Payment, funding, chapters, soundbites, social interactions, live items, trailers, publisher/podroll, and license tags are deliberately omitted because the source data does not justify truthful values for them.

Dates use an RFC 2822-compatible numeric timezone. Artwork is Apple's preferred 3000×3000 RGB JPEG, center-aligned in embedded serif type, with bounded Unicode-aware fitting and distinct spring/fall palettes. Each image identifies the feed as unofficial and includes only the package's short `author-role` field beneath the speaker when one is available.

References:

- [Podcast Standards Project RSS specification](https://github.com/Podcast-Standards-Project/PSP-1-Podcast-RSS-Specification)
- [Podcasting 2.0 namespace](https://github.com/Podcastindex-org/podcast-namespace/blob/main/podcasting2.0.md)
- [Apple Podcasts feed and artwork requirements](https://podcasters.apple.com/support/823-podcast-requirements)
- [Why `encoding/xml` was rejected](https://jayd.ml/2025/06/15/encoding-xml-is-broken-and-no-one-cares.html)
- [The v1 podcasting investigation](https://jayd.ml/2025/05/31/adventures-in-podcasting.html)

GeneralPod is not affiliated with or endorsed by The Church of Jesus Christ of Latter-day Saints.
