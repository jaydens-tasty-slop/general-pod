package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/go-fonts/liberation/liberationserifbold"
	"github.com/go-fonts/liberation/liberationserifregular"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	designSize  = 1400
	artworkSize = 3000
)

func scaled(value int) int { return int(math.Round(float64(value) * artworkSize / designSize)) }

type palette struct {
	background, primary, secondary, accent, soft color.RGBA
}

type artworkRenderer struct {
	regular, bold *opentype.Font
}

var springPalette = palette{
	background: rgba("182338"), primary: rgba("F7E4B3"), secondary: rgba("DBB5D5"),
	accent: rgba("89C8B7"), soft: rgba("6D82B8"),
}

var fallPalette = palette{
	background: rgba("211712"), primary: rgba("F3D49B"), secondary: rgba("D67A48"),
	accent: rgba("9FBA75"), soft: rgba("8D4A3B"),
}

func rgba(hex string) color.RGBA {
	var r, g, b uint8
	_, _ = fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return color.RGBA{r, g, b, 255}
}

func renderArtwork(filename string, episode Episode, regularPath, boldPath string) error {
	renderer, err := newArtworkRenderer(regularPath, boldPath)
	if err != nil {
		return err
	}
	return renderer.render(filename, episode)
}

func newArtworkRenderer(regularPath, boldPath string) (*artworkRenderer, error) {
	regularBytes, err := fontBytes(regularPath, liberationserifregular.TTF)
	if err != nil {
		return nil, err
	}
	boldBytes, err := fontBytes(boldPath, liberationserifbold.TTF)
	if err != nil {
		return nil, err
	}
	regular, err := opentype.Parse(regularBytes)
	if err != nil {
		return nil, err
	}
	bold, err := opentype.Parse(boldBytes)
	if err != nil {
		return nil, err
	}
	return &artworkRenderer{regular: regular, bold: bold}, nil
}

func (renderer *artworkRenderer) render(filename string, episode Episode) error {
	regular, bold := renderer.regular, renderer.bold

	colors := springPalette
	if episode.Conference.Month == 10 {
		colors = fallPalette
	}
	canvas := image.NewRGBA(image.Rect(0, 0, artworkSize, artworkSize))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{colors.background}, image.Point{}, draw.Src)
	drawWaves(canvas, colors, false)
	drawWaves(canvas, colors, true)
	drawFlourish(canvas, scaled(82), colors.primary, false)
	drawFlourish(canvas, artworkSize-scaled(92), colors.primary, true)

	drawFittedCentered(canvas, bold, scaled(44), scaled(36), colors.secondary, scaled(162), "GENERAL CONFERENCE (UNOFFICIAL)", scaled(960))
	conference := strings.ToUpper(episode.Conference.Title)
	if episode.Conference.LanguageISO == "eng" || episode.Conference.LanguageISO == "" {
		conference = strings.ToUpper(englishConferenceName(episode.Conference))
	} else {
		conference = fmt.Sprintf("%d • %s", conferenceNumber(episode.Conference.Year), conference)
	}
	drawFittedCentered(canvas, bold, scaled(56), scaled(38), colors.primary, scaled(230), conference, scaled(1040))
	if episode.Session != "" {
		drawFittedCentered(canvas, regular, scaled(44), scaled(32), colors.secondary, scaled(290), strings.ToUpper(episode.Session), scaled(1000))
	}
	if !episode.Publication.IsZero() {
		drawCentered(canvas, regular, float64(scaled(36)), colors.accent, scaled(342), strings.ToUpper(episode.Publication.Format("January 2, 2006")))
	}

	titleSize, titleLines := fitLines(bold, episode.Title, scaled(116), scaled(52), scaled(1120), 5)
	titleHeight := int(float64(titleSize) * 1.18)
	titleTop := scaled(680) - (len(titleLines)*titleHeight)/2
	for i, line := range titleLines {
		drawCentered(canvas, bold, float64(titleSize), colors.primary, titleTop+i*titleHeight, line)
	}

	if episode.Speaker != "" {
		speakerSize, speakerLines := fitLines(bold, episode.Speaker, scaled(78), scaled(48), scaled(1040), 2)
		lineHeight := int(float64(speakerSize) * 1.2)
		start := scaled(1010) - ((len(speakerLines)-1)*lineHeight)/2
		for i, line := range speakerLines {
			drawCentered(canvas, bold, float64(speakerSize), colors.secondary, start+i*lineHeight, line)
		}
	}
	if episode.SpeakerTitle != "" {
		drawFittedCentered(canvas, regular, scaled(42), scaled(29), colors.accent, scaled(1110), episode.SpeakerTitle, scaled(1000))
	}
	file, err := os.CreateTemp(filepath.Dir(filename), ".artwork-*.jpg")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := jpeg.Encode(file, canvas, &jpeg.Options{Quality: 15}); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, filename)
}

func renderArtworkSet(directory string, episodes []Episode, regularPath, boldPath string) error {
	renderer, err := newArtworkRenderer(regularPath, boldPath)
	if err != nil {
		return err
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 4 {
		workers = 4
	}
	jobs := make(chan Episode)
	errors := make(chan error, workers)
	var group sync.WaitGroup
	for range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			for episode := range jobs {
				filename := filepath.Join(directory, episode.ID+".jpg")
				if err := renderer.render(filename, episode); err != nil {
					select {
					case errors <- fmt.Errorf("artwork for %s: %w", episode.ID, err):
					default:
					}
					return
				}
			}
		}()
	}
	for _, episode := range episodes {
		select {
		case jobs <- episode:
		case err := <-errors:
			close(jobs)
			group.Wait()
			return err
		}
	}
	close(jobs)
	group.Wait()
	select {
	case err := <-errors:
		return err
	default:
		return nil
	}
}

func fontBytes(path string, fallback []byte) ([]byte, error) {
	if path == "" {
		return fallback, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read font %s: %w", path, err)
	}
	return data, nil
}

func fontFace(parsed *opentype.Font, size float64) font.Face {
	face, _ := opentype.NewFace(parsed, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	return face
}

func drawCentered(canvas *image.RGBA, parsed *opentype.Font, size float64, ink color.Color, baseline int, value string) {
	face := fontFace(parsed, size)
	defer face.Close()
	drawer := &font.Drawer{Dst: canvas, Src: image.NewUniform(ink), Face: face}
	width := drawer.MeasureString(value).Ceil()
	drawer.Dot = fixed.P((artworkSize-width)/2, baseline)
	drawer.DrawString(value)
}

func drawFittedCentered(canvas *image.RGBA, parsed *opentype.Font, start, minimum int, ink color.Color, baseline int, value string, maxWidth int) {
	size := start
	for size > minimum {
		face := fontFace(parsed, float64(size))
		width := font.MeasureString(face, value).Ceil()
		face.Close()
		if width <= maxWidth {
			break
		}
		size--
	}
	drawCentered(canvas, parsed, float64(size), ink, baseline, value)
}

func fitLines(parsed *opentype.Font, value string, start, minimum, maxWidth, maxLines int) (int, []string) {
	for size := start; size >= minimum; size -= 2 {
		face := fontFace(parsed, float64(size))
		lines := wrapText(face, value, maxWidth)
		face.Close()
		if len(lines) <= maxLines {
			return size, lines
		}
	}
	face := fontFace(parsed, float64(minimum))
	defer face.Close()
	return minimum, limitLines(face, wrapText(face, value, maxWidth), maxWidth, maxLines)
}

func limitLines(face font.Face, lines []string, maxWidth, maxLines int) []string {
	if len(lines) <= maxLines {
		return lines
	}
	kept := append([]string(nil), lines[:maxLines]...)
	last := strings.Join(lines[maxLines-1:], " ")
	for last != "" && font.MeasureString(face, last+"…").Ceil() > maxWidth {
		_, size := utf8.DecodeLastRuneInString(last)
		last = strings.TrimSpace(last[:len(last)-size])
	}
	kept[maxLines-1] = last + "…"
	return kept
}

func wrapText(face font.Face, value string, maxWidth int) []string {
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}
	measure := func(s string) int { return font.MeasureString(face, s).Ceil() }
	var lines []string
	current := ""
	for _, word := range words {
		if measure(word) > maxWidth {
			parts := splitLongWord(face, word, maxWidth)
			for _, part := range parts {
				if current != "" {
					lines = append(lines, current)
					current = ""
				}
				lines = append(lines, part)
			}
			continue
		}
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if measure(candidate) <= maxWidth {
			current = candidate
		} else {
			lines = append(lines, current)
			current = word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func splitLongWord(face font.Face, word string, maxWidth int) []string {
	var result []string
	remaining := word
	for remaining != "" {
		part := ""
		for len(remaining) > 0 {
			r, n := utf8.DecodeRuneInString(remaining)
			candidate := part + string(r)
			if part != "" && font.MeasureString(face, candidate).Ceil() > maxWidth {
				break
			}
			part = candidate
			remaining = remaining[n:]
		}
		result = append(result, part)
	}
	return result
}

func drawWaves(canvas *image.RGBA, colors palette, bottom bool) {
	start, direction := 0, 1
	if bottom {
		start, direction = artworkSize-1, -1
	}
	bands := []struct {
		height                      int
		amplitude, frequency, phase float64
		ink                         color.RGBA
	}{
		{scaled(42), float64(scaled(12)), 1.4, 0.2, colors.soft},
		{scaled(28), float64(scaled(9)), 1.9, 1.6, colors.accent},
		{scaled(14), float64(scaled(6)), 2.4, 2.8, colors.secondary},
	}
	base := start
	for _, band := range bands {
		for x := 0; x < artworkSize; x++ {
			wave := int(band.amplitude * math.Sin((float64(x)/artworkSize)*math.Pi*2*band.frequency+band.phase))
			y1 := base + direction*wave
			y2 := base + direction*(band.height+wave)
			if y1 > y2 {
				y1, y2 = y2, y1
			}
			draw.Draw(canvas, image.Rect(x, y1, x+1, y2), &image.Uniform{band.ink}, image.Point{}, draw.Src)
		}
		base += direction * (band.height - scaled(12))
	}
}

func drawFlourish(canvas *image.RGBA, y int, ink color.RGBA, flip bool) {
	direction := 1.0
	if flip {
		direction = -1
	}
	// Two mirrored, gently curled cubic strokes with a small center diamond.
	left := [4]image.Point{{scaled(688), y}, {scaled(615), y - int(float64(scaled(42))*direction)}, {scaled(535), y + int(float64(scaled(42))*direction)}, {scaled(455), y}}
	curl := [4]image.Point{{scaled(455), y}, {scaled(390), y - int(float64(scaled(34))*direction)}, {scaled(392), y + int(float64(scaled(60))*direction)}, {scaled(442), y + int(float64(scaled(22))*direction)}}
	drawBezier(canvas, left, ink, scaled(3))
	drawBezier(canvas, curl, ink, scaled(3))
	drawBezier(canvas, mirrorCurve(left), ink, scaled(3))
	drawBezier(canvas, mirrorCurve(curl), ink, scaled(3))
	for dx := -scaled(8); dx <= scaled(8); dx++ {
		for dy := -scaled(8); dy <= scaled(8); dy++ {
			if abs(dx)+abs(dy) <= scaled(8) {
				setDisc(canvas, artworkSize/2+dx, y+dy, scaled(1), ink)
			}
		}
	}
}

func mirrorCurve(curve [4]image.Point) [4]image.Point {
	for i := range curve {
		curve[i].X = artworkSize - curve[i].X
	}
	return curve
}

func drawBezier(canvas *image.RGBA, points [4]image.Point, ink color.RGBA, radius int) {
	for step := 0; step <= 180; step++ {
		t := float64(step) / 180
		u := 1 - t
		x := u*u*u*float64(points[0].X) + 3*u*u*t*float64(points[1].X) + 3*u*t*t*float64(points[2].X) + t*t*t*float64(points[3].X)
		y := u*u*u*float64(points[0].Y) + 3*u*u*t*float64(points[1].Y) + 3*u*t*t*float64(points[2].Y) + t*t*t*float64(points[3].Y)
		setDisc(canvas, int(x), int(y), radius, ink)
	}
}

func setDisc(canvas *image.RGBA, centerX, centerY, radius int, ink color.RGBA) {
	for x := centerX - radius; x <= centerX+radius; x++ {
		for y := centerY - radius; y <= centerY+radius; y++ {
			if (x-centerX)*(x-centerX)+(y-centerY)*(y-centerY) <= radius*radius && image.Pt(x, y).In(canvas.Bounds()) {
				canvas.SetRGBA(x, y, ink)
			}
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func monthName(month int) string {
	if month == 10 {
		return "OCTOBER"
	}
	return "APRIL"
}
