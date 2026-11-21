//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

type Drive struct {
	Path  string `json:"path"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

func Drives() []Drive {
	out := []Drive{}
	for c := 'C'; c <= 'Z'; c++ {
		path := fmt.Sprintf("%c:\\", c)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		var free, total, avail uint64
		p, _ := syscall.UTF16PtrFromString(path)
		proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
		ret, _, _ := proc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
		if ret != 0 {
			out = append(out, Drive{path, free, total})
		} else {
			out = append(out, Drive{Path: path})
		}
	}
	return out
}
func Reveal(path string) error { return exec.Command("explorer.exe", "/select,", path).Start() }
func OpenWindow(url string) error {
	candidates := []string{os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`, os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`, os.Getenv("LOCALAPPDATA") + `\Microsoft\Edge\Application\msedge.exe`}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return exec.Command(path, "--new-window", "--app="+url, "--window-size=1480,940").Start()
		}
	}
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}