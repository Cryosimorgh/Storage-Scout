package scan

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func await(t *testing.T, s *Scanner) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if !s.Status().Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scanner did not finish")
}
func TestScanAndDuplicates(t *testing.T) {
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "nested"), 0755)
	must := func(name, data string) {
		t.Helper()
		if e := os.WriteFile(filepath.Join(root, name), []byte(data), 0644); e != nil {
			t.Fatal(e)
		}
	}
	must("a.txt", "identical")
	must("nested/b.txt", "identical")
	must("video.mp4", "different")
	s := New()
	if e := s.Start(root); e != nil {
		t.Fatal(e)
	}
	await(t, s)
	r := s.Snapshot()
	if r == nil || r.FileCount != 3 || r.TotalBytes != 27 {
		t.Fatalf("unexpected result %+v", r)
	}
	if len(s.ListFolders("nested", 10)) != 1 {
		t.Fatal("missing nested folder")
	}
	if e := s.VerifyDuplicates(); e != nil {
		t.Fatal(e)
	}
	await(t, s)
	r = s.Snapshot()
	if len(r.Duplicates) != 1 || r.DuplicateBytes != 9 {
		t.Fatalf("duplicates incorrect %+v", r.Duplicates)
	}
}
func TestCancellationAndErrors(t *testing.T) {
	s := New()
	if s.Cancel() {
		t.Fatal("cancel without scan")
	}
	if s.Start(filepath.Join(t.TempDir(), "missing")) == nil {
		t.Fatal("should reject nonexistent root")
	}
	if s.VerifyDuplicates() == nil {
		t.Fatal("should reject verify before scan")
	}
}
func TestClassification(t *testing.T) {
	if category(".MP4") != "Videos" || category(".zip") != "Archives" || category(".wat") != "Other" {
		t.Fatal("bad category")
	}
}