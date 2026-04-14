package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/coalaura/plain"
	"github.com/hellsontime/scdl"
)

type Download struct {
	URL      string
	Title    string
	Progress int
	Status   string
}

var (
	mu        sync.Mutex
	downloads []*Download
)

var log = plain.New()

func main() {
	log.Print("> ")

	ctx := context.Background()

	client, err := scdl.NewClient(ctx)
	log.MustFail(err)

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		url := strings.TrimSpace(scanner.Text())
		if url != "" {
			mu.Lock()

			d := &Download{URL: url, Status: "Queued", Progress: 0}

			downloads = append(downloads, d)

			mu.Unlock()

			go processDownload(client, d)
		}

		mu.Lock()
		log.Print("> ")
		mu.Unlock()

		render()
	}
}

func processDownload(client *scdl.Client, d *Download) {
	ctx := context.Background()

	updateStatus(d, "Fetching...", 0)

	track, err := client.GetTrack(ctx, d.URL)
	if err != nil {
		updateStatus(d, "Error", 0)

		return
	}

	if track.Album == "" {
		track.Album = track.Artist
	}

	artistDir := sanitizePath(track.Artist)

	albumDir := filepath.Join(artistDir, sanitizePath(track.Album))

	if err := os.MkdirAll(albumDir, 0755); err != nil {
		updateStatus(d, "Error: Mkdir", 0)

		return
	}

	downloadArtistArt(ctx, artistDir, track)

	mu.Lock()

	if track.Artist != "" && track.Title != "" {
		d.Title = track.Artist + " - " + track.Title
	} else if track.Title != "" {
		d.Title = track.Title
	}

	mu.Unlock()

	render()

	updateStatus(d, "Downloading", 0)

	_, err = client.Download(ctx, track, albumDir, func(downloaded, total int) {
		if total > 0 {
			pct := (downloaded * 100) / total

			updateStatus(d, "Downloading", pct)
		}
	})

	if err != nil {
		updateStatus(d, "Error", 0)
	} else {
		updateStatus(d, "Completed", 100)
	}
}

func updateStatus(d *Download, status string, progress int) {
	mu.Lock()

	d.Status = status
	d.Progress = progress

	mu.Unlock()

	render()
}

func render() {
	mu.Lock()
	defer mu.Unlock()

	if len(downloads) == 0 {
		return
	}

	var b strings.Builder

	b.WriteString("\033[?25l")
	b.WriteString("\033[s")

	fmt.Fprintf(&b, "\033[%dA", len(downloads))

	for _, d := range downloads {
		b.WriteString("\r\033[2K")

		title := d.Title
		if title == "" {
			title = d.URL
		}

		titleRunes := []rune(title)
		if len(titleRunes) > 40 {
			title = string(titleRunes[:37]) + "..."
		}

		bar := makeBar(d.Progress)

		fmt.Fprintf(&b, "%-40s │ %-11s │ %s", title, d.Status, bar)

		b.WriteString("\033[1B")
	}

	b.WriteString("\033[u")
	b.WriteString("\033[?25h")

	log.Print(b.String())
}

func makeBar(percent int) string {
	width := 20

	filled := min(width, max(0, (percent*width)/100))

	var b strings.Builder

	b.WriteString("[")

	for i := range width {
		if i < filled {
			b.WriteString("=")
		} else if i == filled && percent < 100 && percent > 0 {
			b.WriteString(">")
		} else {
			b.WriteString(" ")
		}
	}

	fmt.Fprintf(&b, "] %3d%%", percent)

	return b.String()
}

func sanitizePath(name string) string {
	invalid := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	for _, c := range invalid {
		name = strings.ReplaceAll(name, c, "_")
	}

	return strings.TrimSpace(name)
}

func downloadArtistArt(ctx context.Context, artistDir string, track *scdl.Track) {
	if track.ArtistAvatarURL == "" {
		return
	}

	imgPath := filepath.Join(artistDir, "artist.jpg")
	if _, err := os.Stat(imgPath); err == nil {
		return
	}

	artistURL := strings.Replace(track.ArtistAvatarURL, "-large.", "-t500x500.", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, artistURL, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, track.ArtistAvatarURL, nil)
		if err != nil {
			return
		}

		resp, err = http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}

			return
		}
	}

	defer resp.Body.Close()

	out, err := os.Create(imgPath)
	if err != nil {
		return
	}

	defer out.Close()

	io.Copy(out, resp.Body)
}
