package parse

import (
	"log"
	"sort"
	"strings"

	ctx "github.com/ximilala/viking-go/internal/context"
	"github.com/ximilala/viking-go/internal/vikingfs"
)

// DiffResult holds the outcome of a directory sync operation.
type DiffResult struct {
	AddedFiles   []string `json:"added_files"`
	DeletedFiles []string `json:"deleted_files"`
	UpdatedFiles []string `json:"updated_files"`
	AddedDirs    []string `json:"added_dirs"`
	DeletedDirs  []string `json:"deleted_dirs"`
}

// IsEmpty reports whether no changes were detected.
func (d *DiffResult) IsEmpty() bool {
	return len(d.AddedFiles) == 0 && len(d.DeletedFiles) == 0 &&
		len(d.UpdatedFiles) == 0 && len(d.AddedDirs) == 0 && len(d.DeletedDirs) == 0
}

// TotalChanges returns the total number of changed items.
func (d *DiffResult) TotalChanges() int {
	return len(d.AddedFiles) + len(d.DeletedFiles) + len(d.UpdatedFiles) +
		len(d.AddedDirs) + len(d.DeletedDirs)
}

// SyncConfig configures a top-down recursive sync.
type SyncConfig struct {
	VFS              *vikingfs.VikingFS
	RootURI          string
	TargetURI        string
	ReqCtx           *ctx.RequestContext
	FileChangeStatus map[string]bool // pre-computed change status per file
}

// SyncTopDownRecursive synchronizes a source directory tree (rootURI) into
// a target directory tree (targetURI), detecting added, deleted, and
// updated files along the way.
func SyncTopDownRecursive(cfg SyncConfig) (*DiffResult, error) {
	diff := &DiffResult{}

	if !cfg.VFS.Exists(cfg.TargetURI, cfg.ReqCtx) {
		parentURI := parentOf(cfg.TargetURI)
		if parentURI != "" {
			cfg.VFS.Mkdir(parentURI, cfg.ReqCtx)
		}
		diff.AddedDirs = append(diff.AddedDirs, cfg.RootURI)
		if err := cfg.VFS.Mv(cfg.RootURI, cfg.TargetURI, cfg.ReqCtx); err != nil {
			return diff, err
		}
		return diff, nil
	}

	syncDir(cfg.VFS, cfg.RootURI, cfg.TargetURI, cfg.ReqCtx, cfg.FileChangeStatus, diff)

	cfg.VFS.Rm(cfg.RootURI, true, cfg.ReqCtx)

	return diff, nil
}

func syncDir(vfs *vikingfs.VikingFS, rootDir, targetDir string, reqCtx *ctx.RequestContext, fileChangeStatus map[string]bool, diff *DiffResult) {
	rootFiles, rootDirs := listChildren(vfs, rootDir, reqCtx)
	targetFiles, targetDirs := listChildren(vfs, targetDir, reqCtx)

	allFileNames := mergeKeys(rootFiles, targetFiles)
	sort.Strings(allFileNames)

	for _, name := range allFileNames {
		rootFile := rootFiles[name]
		targetFile := targetFiles[name]

		if rootFile != "" && targetDirs[name] != "" {
			if err := vfs.Rm(targetDirs[name], true, reqCtx); err != nil {
				log.Printf("[SyncDiff] remove conflicting dir: %v", err)
			} else {
				diff.DeletedDirs = append(diff.DeletedDirs, targetDirs[name])
				delete(targetDirs, name)
			}
			targetFile = ""
		}

		if targetFile != "" && rootDirs[name] != "" && rootFile == "" {
			if err := vfs.Rm(targetFile, false, reqCtx); err != nil {
				log.Printf("[SyncDiff] remove conflicting file: %v", err)
			} else {
				diff.DeletedFiles = append(diff.DeletedFiles, targetFile)
			}
			continue
		}

		if targetFile != "" && rootFile == "" {
			if err := vfs.Rm(targetFile, false, reqCtx); err != nil {
				log.Printf("[SyncDiff] delete file: %v", err)
			} else {
				diff.DeletedFiles = append(diff.DeletedFiles, targetFile)
			}
			continue
		}

		if rootFile != "" && targetFile != "" {
			changed := false
			if fileChangeStatus != nil {
				if c, ok := fileChangeStatus[rootFile]; ok {
					changed = c
				}
			} else {
				changed = checkContentChanged(vfs, rootFile, targetFile, reqCtx)
			}
			if changed {
				diff.UpdatedFiles = append(diff.UpdatedFiles, rootFile)
				vfs.Rm(targetFile, false, reqCtx)
				vfs.Mv(rootFile, targetFile, reqCtx)
			}
			continue
		}

		if rootFile != "" && targetFile == "" {
			diff.AddedFiles = append(diff.AddedFiles, rootFile)
			newTargetFile := targetDir + "/" + name
			vfs.Mv(rootFile, newTargetFile, reqCtx)
		}
	}

	allDirNames := mergeKeys(rootDirs, targetDirs)
	sort.Strings(allDirNames)

	for _, name := range allDirNames {
		rootSubdir := rootDirs[name]
		targetSubdir := targetDirs[name]

		if rootSubdir != "" && targetFiles[name] != "" {
			vfs.Rm(targetFiles[name], false, reqCtx)
			diff.DeletedFiles = append(diff.DeletedFiles, targetFiles[name])
			targetSubdir = ""
		}

		if targetSubdir != "" && rootSubdir == "" {
			if err := vfs.Rm(targetSubdir, true, reqCtx); err != nil {
				log.Printf("[SyncDiff] delete dir: %v", err)
			} else {
				diff.DeletedDirs = append(diff.DeletedDirs, targetSubdir)
			}
			continue
		}

		if rootSubdir != "" && targetSubdir == "" {
			diff.AddedDirs = append(diff.AddedDirs, rootSubdir)
			newTargetSubdir := targetDir + "/" + name
			vfs.Mv(rootSubdir, newTargetSubdir, reqCtx)
			continue
		}

		if rootSubdir != "" && targetSubdir != "" {
			syncDir(vfs, rootSubdir, targetSubdir, reqCtx, fileChangeStatus, diff)
		}
	}
}

func listChildren(vfs *vikingfs.VikingFS, dirURI string, reqCtx *ctx.RequestContext) (files, dirs map[string]string) {
	files = make(map[string]string)
	dirs = make(map[string]string)

	entries, err := vfs.Ls(dirURI, reqCtx)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.Name == "" || e.Name == "." || e.Name == ".." {
			continue
		}
		if strings.HasPrefix(e.Name, ".") && e.Name != ".abstract.md" && e.Name != ".overview.md" {
			continue
		}
		itemURI := dirURI + "/" + e.Name
		if e.IsDir {
			dirs[e.Name] = itemURI
		} else {
			files[e.Name] = itemURI
		}
	}
	return
}

func checkContentChanged(vfs *vikingfs.VikingFS, srcURI, targetURI string, reqCtx *ctx.RequestContext) bool {
	srcStat, err := vfs.Stat(srcURI, reqCtx)
	if err != nil {
		return true
	}
	targetStat, err := vfs.Stat(targetURI, reqCtx)
	if err != nil {
		return true
	}
	if srcStat.Size() != targetStat.Size() {
		return true
	}
	srcContent, err := vfs.ReadFile(srcURI, reqCtx)
	if err != nil {
		return true
	}
	targetContent, err := vfs.ReadFile(targetURI, reqCtx)
	if err != nil {
		return true
	}
	return srcContent != targetContent
}

func parentOf(uri string) string {
	idx := strings.LastIndex(uri, "/")
	if idx <= 0 {
		return ""
	}
	return uri[:idx]
}

func mergeKeys(a, b map[string]string) []string {
	seen := make(map[string]bool)
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	return keys
}
