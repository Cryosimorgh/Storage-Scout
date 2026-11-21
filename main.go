package main

import (
	"crypto/rand"
	"embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Cryosimorgh/Storage-Scout/internal/platform"
	"github.com/Cryosimorgh/Storage-Scout/internal/scan"
)

//go:embed ui/*
var ui embed.FS

type app struct {
	scanner *scan.Scanner
	secret  string
	root    string
}

func send(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func bad(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	send(w, map[string]string{"error": msg})
}
func (a *app) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Scout-Key") != a.secret {
			bad(w, 403, "Invalid local session")
			return
		}
		// Only local same-origin requests with a private key can perform actions.
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			bad(w, 403, "Cross-origin request blocked")
			return
		}
		next(w, r)
	}
}
func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { send(w, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /api/drives", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		send(w, map[string]any{"drives": platform.Drives(), "defaultRoot": a.root})
	}))
	mux.HandleFunc("GET /api/status", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		send(w, map[string]any{"progress": a.scanner.Status(), "summary": a.scanner.Summary()})
	}))
	mux.HandleFunc("POST /api/scan", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if err := parse(r, &req); err != nil {
			bad(w, 400, err.Error())
			return
		}
		if strings.TrimSpace(req.Path) == "" {
			bad(w, 400, "Select a drive or enter a folder path")
			return
		}
		if err := a.scanner.Start(req.Path); err != nil {
			bad(w, 409, err.Error())
			return
		}
		send(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /api/cancel", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		send(w, map[string]bool{"cancelled": a.scanner.Cancel()})
	}))
	mux.HandleFunc("POST /api/verify", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		if err := a.scanner.VerifyDuplicates(); err != nil {
			bad(w, 409, err.Error())
			return
		}
		send(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/files", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		send(w, map[string]any{"files": a.scanner.ListFiles(q.Get("view"), q.Get("query"), limit(q.Get("limit"), 1500))})
	}))
	mux.HandleFunc("GET /api/folders", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		send(w, map[string]any{"folders": a.scanner.ListFolders(q.Get("query"), limit(q.Get("limit"), 500))})
	}))
	mux.HandleFunc("GET /api/duplicates", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		send(w, map[string]any{"duplicates": a.scanner.Duplicates()})
	}))
	mux.HandleFunc("POST /api/reveal", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path string `json:"path"`
		}
		if err := parse(r, &req); err != nil {
			bad(w, 400, err.Error())
			return
		}
		// Only open paths returned by our scan, never arbitrary caller-supplied paths.
		if !a.knownPath(req.Path) {
			bad(w, 403, "File is not part of this scan")
			return
		}
		if err := platform.Reveal(req.Path); err != nil {
			bad(w, 500, err.Error())
			return
		}
		send(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /api/export", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		view := r.URL.Query().Get("view")
		if view != "old" && view != "folders" && view != "duplicates" {
			view = "large"
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="storage-scout-%s.csv"`, view))
		csvw := csv.NewWriter(w)
		switch view {
		case "folders":
			_ = csvw.Write([]string{"Folder", "Bytes"})
			_ = a.scanner.ExportFolders(func(f scan.Folder) error { return csvw.Write([]string{f.Path, strconv.FormatInt(f.Size, 10)}) })
		case "duplicates":
			_ = csvw.Write([]string{"File", "Bytes", "SHA256"})
			_ = a.scanner.ExportDuplicates(func(g scan.Duplicate) error {
				for _, p := range g.Paths {
					if err := csvw.Write([]string{p, strconv.FormatInt(g.Size, 10), g.Hash}); err != nil {
						return err
					}
				}
				return nil
			})
		default:
			_ = csvw.Write([]string{"File", "Bytes", "Modified UTC", "Category"})
			_ = a.scanner.ExportFiles(view, func(f scan.File) error {
				return csvw.Write([]string{f.Path, strconv.FormatInt(f.Size, 10), f.Modified.UTC().Format(time.RFC3339), f.Category})
			})
		}
		csvw.Flush()
	}))
	mux.HandleFunc("POST /api/quit", a.authorized(func(w http.ResponseWriter, r *http.Request) {
		send(w, map[string]bool{"ok": true})
		go func() { time.Sleep(150 * time.Millisecond); os.Exit(0) }()
	}))
	mux.Handle("/", http.FileServer(http.FS(ui)))
	return mux
}
func (a *app) knownPath(path string) bool {
	r := a.scanner.Snapshot()
	if r == nil {
		return false
	}
	for _, f := range r.Files {
		if f.Path == path {
			return true
		}
	}
	for _, f := range r.Folders {
		if f.Path == path {
			return true
		}
	}
	return false
}
func limit(v string, def int) int {
	i, _ := strconv.Atoi(v)
	if i < 1 {
		return def
	}
	if i > 3000 {
		return 3000
	}
	return i
}
func parse(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 64*1024)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("one JSON object expected")
	}
	return nil
}
func main() {
	var noBrowser bool
	var port int
	var root string
	flag.BoolVar(&noBrowser, "no-browser", false, "Run local HTTP UI without opening desktop window")
	flag.IntVar(&port, "port", 0, "Local port (default: random)")
	flag.StringVar(&root, "root", platform.DefaultRoot(), "Initial scan path")
	flag.Parse()
	if !filepath.IsAbs(root) {
		abs, e := filepath.Abs(root)
		if e == nil {
			root = abs
		}
	}
	var secretBytes [24]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		log.Fatal(err)
	}
	secret := hex.EncodeToString(secretBytes[:])
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		log.Fatal(err)
	}
	a := &app{scanner: scan.New(), secret: secret, root: root}
	url := fmt.Sprintf("http://%s/ui/?key=%s", listener.Addr().String(), secret)
	server := &http.Server{Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 45 * time.Second}
	fmt.Println("Storage Scout UI:", url)
	go func() {
		if e := server.Serve(listener); e != nil && !errors.Is(e, http.ErrServerClosed) {
			log.Printf("http server: %v", e)
		}
	}()
	if !noBrowser {
		if e := platform.OpenWindow(url); e != nil {
			log.Printf("Unable to open desktop window: %v", e)
		}
	}
	// Keep the local server available while the application is running. Quit from UI.
	select {}
}