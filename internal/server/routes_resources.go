package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ximilala/viking-go/internal/parse"
)

// addResourceRequest mirrors the Python AddResourceRequest.
type addResourceRequest struct {
	Path              string  `json:"path"`
	TempFileID        string  `json:"temp_file_id"`
	To                string  `json:"to"`
	Parent            string  `json:"parent"`
	Reason            string  `json:"reason"`
	Instruction       string  `json:"instruction"`
	Wait              bool    `json:"wait"`
	Timeout           float64 `json:"timeout"`
	Strict            bool    `json:"strict"`
	SourceName        string  `json:"source_name"`
	IgnoreDirs        string  `json:"ignore_dirs"`
	Include           string  `json:"include"`
	Exclude           string  `json:"exclude"`
	PreserveStructure *bool   `json:"preserve_structure,omitempty"`
	WatchInterval     float64 `json:"watch_interval"`
}

func (s *Server) handleAddResource(w http.ResponseWriter, r *http.Request) {
	var req addResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Path == "" && req.TempFileID == "" {
		writeError(w, http.StatusBadRequest, "either 'path' or 'temp_file_id' must be provided")
		return
	}
	if req.To != "" && req.Parent != "" {
		writeError(w, http.StatusBadRequest, "cannot specify both 'to' and 'parent'")
		return
	}

	sourcePath := req.Path
	if req.TempFileID != "" {
		resolved, err := s.resolveTempFileID(req.TempFileID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sourcePath = resolved
	}

	if sourcePath == "" {
		writeError(w, http.StatusBadRequest, "no valid source path")
		return
	}

	reqCtx := s.reqCtx(r)
	tb := parse.NewTreeBuilder(s.vfs)

	scope := "resources"
	sourceFormat := ""
	if strings.HasPrefix(sourcePath, "http://") || strings.HasPrefix(sourcePath, "https://") {
		sourceFormat = "url"
		if isGitURL(sourcePath) {
			sourceFormat = "repository"
		}
	}

	tempURI, err := tb.BuildParseTree(sourcePath, reqCtx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("parse failed: %v", err))
		return
	}

	result, err := tb.FinalizeFromTemp(parse.FinalizeConfig{
		TempDirPath:  tempURI,
		Scope:        scope,
		ToURI:        req.To,
		ParentURI:    req.Parent,
		SourcePath:   sourcePath,
		SourceFormat: sourceFormat,
		ReqCtx:       reqCtx,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("finalize failed: %v", err))
		return
	}

	if s.embQueue != nil && !req.Wait {
		if err := s.embQueue.Enqueue(result.RootURI, reqCtx); err != nil {
			log.Printf("[Resources] enqueue indexing failed: %v", err)
		}
	} else if s.indexer != nil && req.Wait {
		if _, err := s.indexer.IndexDirectory(result.RootURI, reqCtx); err != nil {
			log.Printf("[Resources] sync indexing failed: %v", err)
		}
	}

	if req.WatchInterval > 0 && s.watchMgr != nil {
		if _, err := s.watchMgr.Create(sourcePath, result.RootURI, req.Reason, req.WatchInterval, true); err != nil {
			log.Printf("[Resources] create watch failed: %v", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"uri":           result.RootURI,
			"source_path":   result.SourcePath,
			"source_format": result.SourceFormat,
		},
	})
}

func (s *Server) handleTempUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer file.Close()

	tempDir := s.uploadTempDir()
	s.cleanupOldTempFiles(tempDir, 1*time.Hour)

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".tmp"
	}
	tempName := fmt.Sprintf("upload_%s%s", uuid.New().String()[:12], ext)
	tempPath := filepath.Join(tempDir, tempName)

	dst, err := os.Create(tempPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save uploaded file")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"temp_file_id": tempName,
		},
	})
}

func (s *Server) uploadTempDir() string {
	dir := filepath.Join(s.vfs.RootDir(), ".uploads")
	os.MkdirAll(dir, 0755)
	return dir
}

func (s *Server) cleanupOldTempFiles(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func (s *Server) resolveTempFileID(tempFileID string) (string, error) {
	if strings.Contains(tempFileID, "..") || strings.Contains(tempFileID, "/") {
		return "", fmt.Errorf("invalid temp_file_id")
	}
	path := filepath.Join(s.uploadTempDir(), tempFileID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", fmt.Errorf("temp file not found: %s", tempFileID)
	}
	return path, nil
}

func isGitURL(u string) bool {
	for _, prefix := range []string{"https://github.com/", "https://gitlab.com/", "https://bitbucket.org/"} {
		if strings.HasPrefix(u, prefix) {
			return true
		}
	}
	return strings.HasSuffix(u, ".git")
}

type addSkillRequest struct {
	Path         string `json:"path"`
	TempFileID   string `json:"temp_file_id,omitempty"`
	Name         string `json:"name,omitempty"`
	To           string `json:"to,omitempty"`
	WatchInterval float64 `json:"watch_interval,omitempty"`
}

func (s *Server) handleAddSkill(w http.ResponseWriter, r *http.Request) {
	var req addSkillRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	sourcePath := req.Path
	if req.TempFileID != "" {
		resolved, err := s.resolveTempFileID(req.TempFileID)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sourcePath = resolved
	}

	if sourcePath == "" {
		writeError(w, http.StatusBadRequest, "path or temp_file_id required")
		return
	}

	targetURI := req.To
	if targetURI == "" {
		name := req.Name
		if name == "" {
			name = filepath.Base(sourcePath)
		}
		targetURI = "viking://agent/skills/" + name
	}

	if err := s.vfs.Mkdir(targetURI, nil); err != nil {
		log.Printf("mkdir %s: %v", targetURI, err)
	}

	content, err := os.ReadFile(sourcePath)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot read skill file: %v", err))
		return
	}

	fileURI := targetURI + "/SKILL.md"
	if err := s.vfs.WriteString(fileURI, string(content), s.reqCtx(r)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"result": map[string]any{
			"uri":  targetURI,
			"file": fileURI,
		},
	})
}
