//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"syscall"
)

type Drive struct {
	Path  string `json:"path"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

func Drives() []Drive {
	p := "/"
	var st syscall.Statfs_t
	if syscall.Statfs(p, &st) != nil {
		return []Drive{{Path: p}}
	}
	return []Drive{{Path: p, Free: st.Bavail * uint64(st.Bsize), Total: st.Blocks * uint64(st.Bsize)}}
}
func Reveal(path string) error { return exec.Command("xdg-open", path).Start() }
func OpenWindow(url string) error {
	if p, err := exec.LookPath("chromium"); err == nil {
		return exec.Command(p, "--app="+url, "--window-size=1480,940").Start()
	}
	return exec.Command("xdg-open", url).Start()
}
func DefaultRoot() string {
	d, _ := os.UserHomeDir()
	if d == "" {
		return "/"
	}
	return d
}