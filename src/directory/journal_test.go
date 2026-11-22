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

func TestJournalQueryRoundTrip(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	if err := j.SetUploadInfo("a.txt", UploadInfo{
		Size: 42, ModTime: 123, ContentChecksum: "SHA1:abc", URL: "https://example.com/tus/1", Valid: true,
	}); err != nil {
		t.Fatal(err)
	}
	up, err := j.UploadInfo("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !up.Valid || up.Size != 42 || up.URL != "https://example.com/tus/1" {
		t.Fatalf("UploadInfo = %+v", up)
	}
	all, err := j.AllUploadInfos()
	if err != nil || len(all) != 1 || all[0].Path != "a.txt" {
		t.Fatalf("AllUploadInfos = %+v, %v", all, err)
	}

	if err := j.SetDownloadInfo("b.txt", DownloadInfo{TmpFile: "b.tmp", Etag: "etag", ErrorCount: 2, Valid: true}); err != nil {
		t.Fatal(err)
	}
	down, err := j.DownloadInfo("b.txt")
	if err != nil || !down.Valid || down.TmpFile != "b.tmp" || down.Etag != "etag" {
		t.Fatalf("DownloadInfo = %+v, %v", down, err)
	}

	if err := j.SetErrorBlacklistEntry(BlacklistEntry{Path: "c.txt", RetryCount: 3, ErrorString: "denied"}); err != nil {
		t.Fatal(err)
	}
	bl, ok, err := j.ErrorBlacklistEntry("c.txt")
	if err != nil || !ok || bl.RetryCount != 3 || bl.ErrorString != "denied" {
		t.Fatalf("ErrorBlacklistEntry = %+v, %v, %v", bl, ok, err)
	}

	if err := j.SetConflictRecord(ConflictRecord{Path: "d.txt", BaseFileID: "id", BaseEtag: "e", BaseModtime: 7, BasePath: "old.txt"}); err != nil {
		t.Fatal(err)
	}
	conflict, ok, err := j.ConflictRecord("d.txt")
	if err != nil || !ok || conflict.BaseFileID != "id" || conflict.BasePath != "old.txt" {
		t.Fatalf("ConflictRecord = %+v, %v, %v", conflict, ok, err)
	}

	if err := j.SetSelectiveSync("excluded/", 1); err != nil {
		t.Fatal(err)
	}
	list, err := j.SelectiveSync(1)
	if err != nil || len(list) != 1 || list[0] != "excluded/" {
		t.Fatalf("SelectiveSync = %v, %v", list, err)
	}

	if err := j.SetVersion(Version{Major: 1, Minor: 2, Patch: 3, Custom: "x"}); err != nil {
		t.Fatal(err)
	}
	version, err := j.Version()
	if err != nil || version.Major != 1 || version.Custom != "x" {
		t.Fatalf("Version = %+v, %v", version, err)
	}
}
