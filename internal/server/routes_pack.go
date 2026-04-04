package server

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- Pack handlers (export/import .ovpack) ---

type packExportRequest struct {
	URI string `json:"uri"`
	To  string `json:"to"`
}

func (s *Server) handlePackExport(w http.ResponseWriter, r *http.Request) {
	var req packExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.URI == "" || req.To == "" {
		writeError(w, http.StatusBadRequest, "uri and to are required")
		return
	}

	rc := s.reqCtx(r)
	localDir, err := s.vfs.URIToPath(req.URI, rc)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := os.Stat(localDir)
	if err != nil || !info.IsDir() {
		writeError(w, http.StatusNotFound, fmt.Sprintf("source not found or not a directory: %s", req.URI))
		return
	}

	outPath := req.To
	if !strings.HasSuffix(outPath, ".ovpack") {
		outPath += ".ovpack"
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create output dir: %v", err))
		return
	}

	f, err := os.Create(outPath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("create output file: %v", err))
		return
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	manifest := map[string]any{
		"format":     "ovpack/v1",
		"source_uri": req.URI,
		"exported_at": time.Now().UTC().Format(time.RFC3339),
	}
	manifestJSON, _ := json.Marshal(manifest)
	_ = tw.WriteHeader(&tar.Header{
		Name:    ".manifest.json",
		Size:    int64(len(manifestJSON)),
		Mode:    0644,
		ModTime: time.Now(),
	})
	tw.Write(manifestJSON)

	var fileCount int
	err = filepath.WalkDir(localDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(localDir, path)
		if relPath == "." {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil
		}
		header.Name = relPath

		if d.IsDir() {
			header.Name += "/"
		}

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if !d.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer file.Close()
			io.Copy(tw, file)
			fileCount++
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("pack error: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"file":   outPath,
		"files":  fileCount,
	})
}

type packImportRequest struct {
	FilePath  string `json:"file_path"`
	Parent    string `json:"parent"`
	Force     bool   `json:"force"`
	Vectorize bool   `json:"vectorize"`
}

func (s *Server) handlePackImport(w http.ResponseWriter, r *http.Request) {
	var req packImportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.FilePath == "" || req.Parent == "" {
		writeError(w, http.StatusBadRequest, "file_path and parent are required")
		return
	}

	rc := s.reqCtx(r)
	parentDir, err := s.vfs.URIToPath(req.Parent, rc)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	f, err := os.Open(req.FilePath)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("pack file not found: %v", err))
		return
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("not a valid gzip archive: %v", err))
		return
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var fileCount int

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("tar read error: %v", err))
			return
		}

		if header.Name == ".manifest.json" {
			io.Copy(io.Discard, tr)
			continue
		}

		target := filepath.Join(parentDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, parentDir) {
			continue
		}

		switch header.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, 0755)
		case tar.TypeReg:
			if !req.Force {
				if _, err := os.Stat(target); err == nil {
					io.Copy(io.Discard, tr)
					continue
				}
			}
			os.MkdirAll(filepath.Dir(target), 0755)
			out, err := os.Create(target)
			if err != nil {
				continue
			}
			io.Copy(out, tr)
			out.Close()
			fileCount++
		}
	}

	if req.Vectorize && s.indexer != nil {
		go func() {
			if _, err := s.indexer.IndexDirectory(req.Parent, rc); err != nil {
				fmt.Printf("[Pack] vectorize after import error: %v\n", err)
			}
		}()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uri":    req.Parent,
		"files":  fileCount,
	})
}
