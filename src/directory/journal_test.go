package directory

import (
	"path/filepath"
	"testing"
)

func TestOpenJournalCreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.db")

	for i := 0; i < 2; i++ { // opening twice must be safe
		j, err := OpenJournal(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i+1, err)
		}
		rows, err := j.db.Query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			got[name] = true
		}
		rows.Close()
		for _, table := range []string{"metadata", "downloadinfo", "uploadinfo", "blacklist", "selectivesync", "checksumtype", "conflicts", "version"} {
			if !got[table] {
				t.Errorf("open #%d: missing table %q", i+1, table)
			}
		}
		if err := j.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
