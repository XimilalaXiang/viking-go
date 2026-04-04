package parse

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ximilala/viking-go/internal/vlm"
)

// VideoFileParser extracts content from video files by:
// 1. Extracting key frames using ffmpeg (if available)
// 2. Understanding frames via VLM
// 3. Optionally extracting audio transcript via Whisper
type VideoFileParser struct {
	vlmClient   *vlm.Client
	audioCfg    *AudioConfig
	maxFrames   int
}

// NewVideoFileParser creates a video parser with optional VLM and audio support.
func NewVideoFileParser(vlmClient *vlm.Client, audioCfg *AudioConfig) *VideoFileParser {
	return &VideoFileParser{
		vlmClient: vlmClient,
		audioCfg:  audioCfg,
		maxFrames: 5,
	}
}

func (p *VideoFileParser) Name() string { return "video" }

func (p *VideoFileParser) Extensions() []string {
	return []string{".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv", ".webm"}
}

func (p *VideoFileParser) Parse(source string, isPath bool) (*ParseResult, error) {
	if !isPath {
		return nil, fmt.Errorf("video parsing requires a file path")
	}

	title := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	var sections []string

	if p.vlmClient != nil && hasFFmpeg() {
		frames, err := extractFrames(source, p.maxFrames)
		if err == nil && len(frames) > 0 {
			for i, framePath := range frames {
				result, err := p.vlmClient.UnderstandImage(framePath, fmt.Sprintf("Frame %d of video: %s", i+1, title), "")
				if err != nil {
					continue
				}
				sections = append(sections, fmt.Sprintf("### Frame %d\n\n%s", i+1, result.Detail))
			}
			cleanupFrames(frames)
		}
	}

	if p.audioCfg != nil && p.audioCfg.APIKey != "" {
		audioParser := NewAudioFileParser(p.audioCfg)
		audioResult, err := audioParser.Parse(source, true)
		if err == nil && audioResult.Content != "" {
			sections = append(sections, fmt.Sprintf("## Audio Transcript\n\n%s", audioResult.Content))
		}
	}

	if len(sections) == 0 {
		return &ParseResult{
			Title:    title,
			Abstract: fmt.Sprintf("Video: %s", title),
			Content:  fmt.Sprintf("[Video file: %s]\nNo VLM or ffmpeg available for video understanding.", filepath.Base(source)),
			Format:   "video",
		}, nil
	}

	content := fmt.Sprintf("# %s\n\n%s", title, strings.Join(sections, "\n\n"))
	abstract := truncateStr(title, 200)

	return &ParseResult{
		Title:    title,
		Abstract: abstract,
		Content:  content,
		Format:   "video",
	}, nil
}

func hasFFmpeg() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

func extractFrames(videoPath string, maxFrames int) ([]string, error) {
	dir, err := filepath.Abs(filepath.Dir(videoPath))
	if err != nil {
		return nil, err
	}

	pattern := filepath.Join(dir, ".viking_frame_%03d.jpg")

	interval := 1.0
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		videoPath,
	)
	out, err := cmd.Output()
	if err == nil {
		var duration float64
		if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &duration); err == nil && duration > 0 {
			interval = duration / float64(maxFrames+1)
			if interval < 1 {
				interval = 1
			}
		}
	}

	extractCmd := exec.Command("ffmpeg",
		"-i", videoPath,
		"-vf", fmt.Sprintf("fps=1/%.1f", interval),
		"-frames:v", fmt.Sprintf("%d", maxFrames),
		"-q:v", "2",
		"-y",
		pattern,
	)
	if err := extractCmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg frame extraction: %w", err)
	}

	var frames []string
	for i := 1; i <= maxFrames; i++ {
		path := fmt.Sprintf(filepath.Join(dir, ".viking_frame_%03d.jpg"), i)
		if fileExists(path) {
			frames = append(frames, path)
		}
	}

	return frames, nil
}

func cleanupFrames(frames []string) {
	for _, f := range frames {
		_ = removeFile(f)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func removeFile(path string) error {
	return os.Remove(path)
}
