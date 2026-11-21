// Package scan provides read-only disk inventory and duplicate verification.
// File age means time since last modification, never proof of disuse.
package scan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type File struct {
	Path      string    `json:"path"`
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
	Extension string    `json:"extension"`
	Category  string    `json:"category"`
}
type Folder struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}
type TypeInfo struct {
	Category string `json:"category"`
	Bytes    int64  `json:"bytes"`
	Count    int    `json:"count"`
}
type Duplicate struct {
	Size  int64    `json:"size"`
	Paths []string `json:"paths"`
	Hash  string   `json:"hash"`
}
type Progress struct {
	Running       bool      `json:"running"`
	Phase         string    `json:"phase"`
	Path          string    `json:"path"`
	Files         int       `json:"files"`
	Folders       int       `json:"folders"`
	Bytes         int64     `json:"bytes"`
	Errors        int       `json:"errors"`
	Started       time.Time `json:"started"`
	Updated       time.Time `json:"updated"`
	VerifiedFiles int       `json:"verifiedFiles"`
	VerifyTotal   int       `json:"verifyTotal"`
}
type Result struct {
	Root           string      `json:"root"`
	TotalBytes     int64       `json:"totalBytes"`
	FileCount      int         `json:"fileCount"`
	FolderCount    int         `json:"folderCount"`
	OldBytes       int64       `json:"oldBytes"`
	OldCount       int         `json:"oldCount"`
	DuplicateBytes int64       `json:"duplicateBytes"`
	Duplicates     []Duplicate `json:"duplicates"`
	Types          []TypeInfo  `json:"types"`
	Files          []File      `json:"-"`
	Folders        []Folder    `json:"-"`
	Finished       time.Time   `json:"finished"`
}

var ErrCancelled = errors.New("scan cancelled")

type Scanner struct {
	mu       sync.RWMutex
	progress Progress
	result   *Result
	cancel   context.CancelFunc
}

func New() *Scanner                 { return &Scanner{} }
func (s *Scanner) Status() Progress { s.mu.RLock(); defer s.mu.RUnlock(); return s.progress }

// Summary returns an immutable copy safe for concurrent HTTP encoding.
func (s *Scanner) Summary() *Result {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return nil
	}
	result := *s.result
	result.Files = nil
	result.Folders = nil
	result.Types = append([]TypeInfo(nil), s.result.Types...)
	result.Duplicates = append([]Duplicate(nil), s.result.Duplicates...)
	return &result
}
func (s *Scanner) Snapshot() *Result { s.mu.RLock(); defer s.mu.RUnlock(); return s.result }
func (s *Scanner) Cancel() bool {
	s.mu.RLock()
	cancel := s.cancel
	s.mu.RUnlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}
func (s *Scanner) Start(root string) error {
	full, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return err
	}
	stat, err := os.Stat(full)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return errors.New("path is not a directory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress.Running {
		return errors.New("a task is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.progress = Progress{Running: true, Phase: "scanning", Path: full, Started: time.Now(), Updated: time.Now()}
	s.result = nil
	go s.walk(ctx, full)
	return nil
}
func (s *Scanner) update(p Progress) { s.mu.Lock(); s.progress = p; s.mu.Unlock() }
func (s *Scanner) finish(p Progress, r *Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Running = false
	p.Updated = time.Now()
	s.progress = p
	s.cancel = nil
	s.result = r
}
func category(ext string) string {
	switch strings.ToLower(ext) {
	case ".zip", ".7z", ".rar", ".tar", ".gz", ".iso", ".img":
		return "Archives"
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".wmv":
		return "Videos"
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff", ".raw", ".heic":
		return "Images"
	case ".mp3", ".wav", ".flac", ".ogg", ".m4a":
		return "Audio"
	case ".pdf", ".doc", ".docx", ".pptx", ".xlsx", ".txt", ".md", ".csv":
		return "Documents"
	case ".exe", ".msi", ".dll", ".sys", ".appx", ".msix":
		return "Applications"
	case ".db", ".sqlite", ".sqlite3", ".bak", ".vhd", ".vhdx":
		return "Data & backups"
	case ".go", ".rs", ".zig", ".cpp", ".h", ".cs", ".js", ".ts", ".py", ".json", ".html", ".css":
		return "Code"
	default:
		return "Other"
	}
}
func (s *Scanner) walk(ctx context.Context, root string) {
	p := s.Status()
	r := &Result{Root: root}
	folderMap := map[string]int64{}
	typeMap := map[string]*TypeInfo{}
	last := time.Now()
	oldBefore := time.Now().AddDate(-1, 0, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if ctx.Err() != nil {
			return ErrCancelled
		}
		if e != nil {
			p.Errors++
			return nil
		}
		// WalkDir never traverses symlinks; explicitly omit their byte sizes.
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			p.Folders++
			folderMap[path] += 0
			return nil
		}
		if !d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				p.Errors++
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			p.Errors++
			return nil
		}
		if info.Size() < 0 {
			p.Errors++
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		cat := category(ext)
		f := File{Path: path, Name: d.Name(), Size: info.Size(), Modified: info.ModTime(), Extension: ext, Category: cat}
		r.Files = append(r.Files, f)
		p.Files++
		p.Bytes += f.Size
		if info.ModTime().Before(oldBefore) {
			r.OldBytes += f.Size
			r.OldCount++
		}
		if typeMap[cat] == nil {
			typeMap[cat] = &TypeInfo{Category: cat}
		}
		typeMap[cat].Bytes += f.Size
		typeMap[cat].Count++
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			folderMap[dir] += f.Size
			if dir == root || dir == filepath.Dir(dir) {
				break
			}
		}
		if time.Since(last) > 130*time.Millisecond {
			p.Path = path
			p.Updated = time.Now()
			s.update(p)
			last = time.Now()
		}
		return nil
	})
	if errors.Is(err, ErrCancelled) {
		p.Phase = "cancelled"
		s.finish(p, nil)
		return
	}
	if err != nil {
		p.Errors++
		p.Phase = "error: " + err.Error()
		s.finish(p, nil)
		return
	}
	r.TotalBytes = p.Bytes
	r.FileCount = p.Files
	r.FolderCount = p.Folders
	r.Finished = time.Now()
	for path, size := range folderMap {
		if path == root {
			continue
		}
		r.Folders = append(r.Folders, Folder{Path: path, Name: filepath.Base(path), Size: size})
	}
	sort.Slice(r.Folders, func(i, j int) bool { return r.Folders[i].Size > r.Folders[j].Size })
	for _, typ := range typeMap {
		r.Types = append(r.Types, *typ)
	}
	sort.Slice(r.Types, func(i, j int) bool { return r.Types[i].Bytes > r.Types[j].Bytes })
	sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Size > r.Files[j].Size })
	p.Phase = "complete"
	p.Path = root
	s.finish(p, r)
}
func (s *Scanner) VerifyDuplicates() error {
	s.mu.Lock()
	if s.progress.Running {
		s.mu.Unlock()
		return errors.New("a task is already running")
	}
	if s.result == nil {
		s.mu.Unlock()
		return errors.New("scan a folder first")
	}
	// Work on existing immutable list; only duplicate fields updated at completion.
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	p := s.progress
	p.Running = true
	p.Phase = "verifying"
	p.VerifiedFiles = 0
	p.VerifyTotal = 0
	s.progress = p
	files := s.result.Files
	s.mu.Unlock()
	go s.verify(ctx, files, p)
	return nil
}
func (s *Scanner) verify(ctx context.Context, files []File, p Progress) {
	bySize := make(map[int64][]File)
	for _, f := range files {
		if f.Size > 0 {
			bySize[f.Size] = append(bySize[f.Size], f)
		}
	}
	groups := make([][]File, 0)
	for _, v := range bySize {
		if len(v) > 1 {
			groups = append(groups, v)
			p.VerifyTotal += len(v)
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0].Size > groups[j][0].Size })
	s.update(p)
	var duplicates []Duplicate
	var wasted int64
	last := time.Now()
	for _, g := range groups {
		matched := make(map[string][]string)
		for _, f := range g {
			if ctx.Err() != nil {
				p.Phase = "cancelled"
				s.finishVerify(p, nil, 0)
				return
			}
			h, err := hashFile(ctx, f)
			if err != nil {
				if ctx.Err() != nil {
					p.Phase = "cancelled"
					s.finishVerify(p, nil, 0)
					return
				}
				p.Errors++
			} else {
				matched[h] = append(matched[h], f.Path)
			}
			p.VerifiedFiles++
			if time.Since(last) > 120*time.Millisecond {
				p.Path = f.Path
				p.Updated = time.Now()
				s.update(p)
				last = time.Now()
			}
		}
		for h, paths := range matched {
			if len(paths) > 1 {
				sort.Strings(paths)
				duplicates = append(duplicates, Duplicate{Size: g[0].Size, Paths: paths, Hash: h})
				wasted += g[0].Size * int64(len(paths)-1)
			}
		}
	}
	sort.Slice(duplicates, func(i, j int) bool {
		return duplicates[i].Size*int64(len(duplicates[i].Paths)-1) > duplicates[j].Size*int64(len(duplicates[j].Paths)-1)
	})
	p.Phase = "complete"
	p.Path = ""
	s.finishVerify(p, duplicates, wasted)
}
func hashFile(ctx context.Context, original File) (string, error) {
	info, statErr := os.Stat(original.Path)
	if statErr != nil {
		return "", statErr
	}
	if !info.Mode().IsRegular() || info.Size() != original.Size || !info.ModTime().Equal(original.Modified) {
		return "", errors.New("file changed since scan")
	}
	f, err := os.Open(original.Path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hash := sha256.New()
	buf := make([]byte, 1024*1024)
	for {
		if ctx.Err() != nil {
			return "", ErrCancelled
		}
		n, e := f.Read(buf)
		if n > 0 {
			_, _ = hash.Write(buf[:n])
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			return "", e
		}
	}

	after, e := f.Stat()
	if e != nil || after.Size() != original.Size || !after.ModTime().Equal(original.Modified) {
		return "", errors.New("file changed during verification")
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (s *Scanner) finishVerify(p Progress, d []Duplicate, wasted int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.Running = false
	p.Updated = time.Now()
	s.progress = p
	s.cancel = nil
	if d != nil && s.result != nil {
		s.result.Duplicates = d
		s.result.DuplicateBytes = wasted
	}
}

// ListFiles returns a bounded copy matching the requested view; it cannot mutate scan state.
func (s *Scanner) ListFiles(view, query string, limit int) []File {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return []File{}
	}
	if limit < 1 || limit > 3000 {
		limit = 3000
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]File, 0, min(limit, 3000))
	oldBefore := time.Now().AddDate(-1, 0, 0)
	for _, f := range s.result.Files {
		if view == "old" && !f.Modified.Before(oldBefore) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(f.Path), query) {
			continue
		}
		out = append(out, f)
		if len(out) >= limit {
			break
		}
	}
	return out
}
func (s *Scanner) ListFolders(query string, limit int) []Folder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return []Folder{}
	}
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]Folder, 0, min(limit, 1000))
	for _, f := range s.result.Folders {
		if query == "" || strings.Contains(strings.ToLower(f.Path), query) {
			out = append(out, f)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
func (s *Scanner) Duplicates() []Duplicate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return []Duplicate{}
	}
	return append([]Duplicate{}, s.result.Duplicates...)
}

// ExportFiles streams every matching file in rank order. This is intentionally
// separate from the bounded UI results.
func (s *Scanner) ExportFiles(view string, emit func(File) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return errors.New("no completed scan")
	}
	before := time.Now().AddDate(-1, 0, 0)
	for _, f := range s.result.Files {
		if view == "old" && !f.Modified.Before(before) {
			continue
		}
		if err := emit(f); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scanner) ExportFolders(emit func(Folder) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return errors.New("no completed scan")
	}
	for _, f := range s.result.Folders {
		if err := emit(f); err != nil {
			return err
		}
	}
	return nil
}
func (s *Scanner) ExportDuplicates(emit func(Duplicate) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.result == nil {
		return errors.New("no completed scan")
	}
	for _, d := range s.result.Duplicates {
		if err := emit(d); err != nil {
			return err
		}
	}
	return nil
}