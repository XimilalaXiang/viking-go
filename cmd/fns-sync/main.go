package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type FNSConfig struct {
	FNSAPI      string
	FNSToken    string
	Vault       string
	VikingAPI   string
	BatchSize   int
	Concurrency int
	DryRun      bool
	Since       string
}

type FNSNoteListItem struct {
	Path      string `json:"path"`
	PathHash  string `json:"pathHash"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updatedAt"`
}

type FNSNoteDetail struct {
	Path        string `json:"path"`
	PathHash    string `json:"pathHash"`
	Content     string `json:"content"`
	ContentHash string `json:"contentHash"`
	UpdatedAt   string `json:"updatedAt"`
}

type FNSListResponse struct {
	Code    int    `json:"code"`
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		List  []FNSNoteListItem `json:"list"`
		Pager struct {
			Page      int `json:"page"`
			PageSize  int `json:"pageSize"`
			TotalRows int `json:"totalRows"`
		} `json:"pager"`
	} `json:"data"`
}

type FNSNoteResponse struct {
	Code    int           `json:"code"`
	Status  bool          `json:"status"`
	Message string        `json:"message"`
	Data    FNSNoteDetail `json:"data"`
}

func main() {
	cfg := FNSConfig{}
	flag.StringVar(&cfg.FNSAPI, "fns-api", "http://127.0.0.1:9000", "Fast Note Sync API URL")
	flag.StringVar(&cfg.FNSToken, "fns-token", "", "FNS auth token")
	flag.StringVar(&cfg.Vault, "vault", "000-Knowledge", "FNS vault name")
	flag.StringVar(&cfg.VikingAPI, "viking-api", "http://127.0.0.1:6920", "viking-go API URL")
	flag.IntVar(&cfg.BatchSize, "batch", 100, "Page size for listing notes")
	flag.IntVar(&cfg.Concurrency, "concurrency", 5, "Concurrent note fetches")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "Only list notes, don't sync")
	flag.StringVar(&cfg.Since, "since", "", "Only sync notes updated after this time (YYYY-MM-DD HH:MM:SS)")
	flag.Parse()

	if cfg.FNSToken == "" {
		cfg.FNSToken = os.Getenv("FNS_TOKEN")
	}
	if cfg.FNSToken == "" {
		log.Fatal("FNS token required: use -fns-token or FNS_TOKEN env")
	}

	client := &http.Client{Timeout: 30 * time.Second}

	log.Printf("FNS Sync: %s vault=%s → %s", cfg.FNSAPI, cfg.Vault, cfg.VikingAPI)

	allNotes, err := listAllNotes(client, cfg)
	if err != nil {
		log.Fatalf("Failed to list notes: %v", err)
	}
	log.Printf("Found %d notes in vault %s", len(allNotes), cfg.Vault)

	filtered := filterNotes(allNotes, cfg.Since)
	log.Printf("After filter: %d notes to sync", len(filtered))

	if cfg.DryRun {
		for _, n := range filtered[:min(20, len(filtered))] {
			fmt.Printf("  %s (%d bytes, updated %s)\n", n.Path, n.Size, n.UpdatedAt)
		}
		if len(filtered) > 20 {
			fmt.Printf("  ... and %d more\n", len(filtered)-20)
		}
		return
	}

	ensureVikingDirs(client, cfg, filtered)

	synced, skipped, errors := 0, 0, 0
	sem := make(chan struct{}, cfg.Concurrency)

	type result struct {
		ok      bool
		skipped bool
	}
	results := make(chan result, len(filtered))

	for i, note := range filtered {
		sem <- struct{}{}
		go func(n FNSNoteListItem, idx int) {
			defer func() { <-sem }()

			if n.Size == 0 {
				results <- result{ok: true, skipped: true}
				return
			}

			detail, err := fetchNote(client, cfg, n.Path)
			if err != nil {
				log.Printf("[%d/%d] ERROR fetching %s: %v", idx+1, len(filtered), n.Path, err)
				results <- result{ok: false}
				return
			}

			if detail.Content == "" {
				results <- result{ok: true, skipped: true}
				return
			}

			uri := notePathToURI(n.Path)
			if err := writeToViking(client, cfg, uri, detail.Content); err != nil {
				log.Printf("[%d/%d] ERROR writing %s: %v", idx+1, len(filtered), uri, err)
				results <- result{ok: false}
				return
			}

			if idx%100 == 0 {
				log.Printf("[%d/%d] synced %s", idx+1, len(filtered), n.Path)
			}
			results <- result{ok: true}
		}(note, i)
	}

	for range filtered {
		r := <-results
		if r.skipped {
			skipped++
		} else if r.ok {
			synced++
		} else {
			errors++
		}
	}

	log.Printf("Sync complete: %d synced, %d skipped (empty), %d errors", synced, skipped, errors)

	log.Println("Triggering reindex on viking-go...")
	if err := triggerReindex(client, cfg, "viking://fns"); err != nil {
		log.Printf("Reindex error: %v (you may need to manually reindex)", err)
	} else {
		log.Println("Reindex triggered successfully")
	}
}

func listAllNotes(client *http.Client, cfg FNSConfig) ([]FNSNoteListItem, error) {
	var all []FNSNoteListItem
	page := 1
	for {
		u := fmt.Sprintf("%s/api/notes?vault=%s&page=%d&pageSize=%d",
			cfg.FNSAPI, url.QueryEscape(cfg.Vault), page, cfg.BatchSize)
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("Token", cfg.FNSToken)
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list page %d: %w", page, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var listResp FNSListResponse
		if err := json.Unmarshal(body, &listResp); err != nil {
			return nil, fmt.Errorf("parse page %d: %w (body: %s)", page, err, string(body[:min(200, len(body))]))
		}
		if !listResp.Status {
			return nil, fmt.Errorf("API error page %d: %s", page, listResp.Message)
		}

		all = append(all, listResp.Data.List...)
		total := listResp.Data.Pager.TotalRows
		if len(all) >= total || len(listResp.Data.List) == 0 {
			break
		}
		page++
		if page%10 == 0 {
			log.Printf("  listed %d/%d notes...", len(all), total)
		}
	}
	return all, nil
}

func filterNotes(notes []FNSNoteListItem, since string) []FNSNoteListItem {
	if since == "" {
		return notes
	}
	sinceTime, err := time.Parse("2006-01-02 15:04:05", since)
	if err != nil {
		sinceTime, err = time.Parse("2006-01-02", since)
		if err != nil {
			log.Printf("Warning: invalid --since format %q, syncing all", since)
			return notes
		}
	}
	var filtered []FNSNoteListItem
	for _, n := range notes {
		t, err := time.Parse("2006-01-02 15:04:05", n.UpdatedAt)
		if err != nil {
			filtered = append(filtered, n)
			continue
		}
		if t.After(sinceTime) || t.Equal(sinceTime) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

func fetchNote(client *http.Client, cfg FNSConfig, path string) (*FNSNoteDetail, error) {
	u := fmt.Sprintf("%s/api/note?vault=%s&path=%s",
		cfg.FNSAPI, url.QueryEscape(cfg.Vault), url.QueryEscape(path))
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Token", cfg.FNSToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var noteResp FNSNoteResponse
	if err := json.Unmarshal(body, &noteResp); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if !noteResp.Status {
		return nil, fmt.Errorf("API: %s", noteResp.Message)
	}
	return &noteResp.Data, nil
}

func notePathToURI(path string) string {
	path = strings.TrimSuffix(path, ".md")
	path = strings.ReplaceAll(path, " ", "_")
	clean := strings.Map(func(r rune) rune {
		if r == '/' || r == '-' || r == '_' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r > 127 {
			return r
		}
		return '_'
	}, path)
	return "viking://fns/" + clean + ".md"
}

func ensureVikingDirs(client *http.Client, cfg FNSConfig, notes []FNSNoteListItem) {
	dirs := make(map[string]bool)
	dirs["viking://fns"] = true
	for _, n := range notes {
		uri := notePathToURI(n.Path)
		parts := strings.Split(uri, "/")
		for i := 3; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		body := fmt.Sprintf(`{"uri":"%s"}`, dir)
		req, _ := http.NewRequest("POST", cfg.VikingAPI+"/api/v1/fs/mkdir",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
		}
	}
}

func writeToViking(client *http.Client, cfg FNSConfig, uri, content string) error {
	payload, _ := json.Marshal(map[string]string{"uri": uri, "content": content})
	req, _ := http.NewRequest("POST", cfg.VikingAPI+"/api/v1/content/write",
		strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func triggerReindex(client *http.Client, cfg FNSConfig, uri string) error {
	payload, _ := json.Marshal(map[string]string{"uri": uri})
	req, _ := http.NewRequest("POST", cfg.VikingAPI+"/api/v1/content/reindex",
		strings.NewReader(string(payload)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	log.Printf("Reindex response: %s", string(body))
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
