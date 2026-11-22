package directory

import (
	"os"
	"path/filepath"
	"testing"

	"example.com/project/src/journal"
)

func TestLocalInfo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	info := NewLocalInfo(file)
	if !info.IsValid() || info.Name() != "a.txt" {
		t.Fatalf("Name = %q, valid = %t", info.Name(), info.IsValid())
	}
	if info.Type() != journal.ItemTypeFile || !info.IsValid() {
		t.Errorf("Type = %v, want File", info.Type())
	}
	if info.Size() != 5 {
		t.Errorf("Size = %d, want 5", info.Size())
	}
	if info.Inode() == 0 {
		t.Error("Inode = 0; rename detection needs a real inode")
	}
	if info.ModTime() == 0 {
		t.Error("ModTime = 0")
	}

	if d := NewLocalInfo(dir); !d.IsDirectory() {
		t.Errorf("directory Type = %v", d.Type())
	}
	if missing := NewLocalInfo(filepath.Join(dir, "nope")); missing.IsValid() {
		t.Error("a failed stat must produce an invalid LocalInfo")
	}
}
