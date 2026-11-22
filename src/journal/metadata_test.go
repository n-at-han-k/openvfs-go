package journal

import (
	"path/filepath"
	"testing"
)

func TestFileRecordRoundTrip(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	want := NewSyncJournalFileRecord("a/b.txt", ItemTypeFile)
	want.Inode = 4242
	want.ModTime = 1700000000
	want.HasModTime = true
	want.Etag = "abc123"
	want.FileID = "fileid-1"
	want.FileSize = 99
	want.RemotePerm = RemotePermissionsFromServerString("WDNVCK")
	want.ChecksumHeader = "SHA1:deadbeef"
	want.ServerHasIgnoredFiles = true
	want.HasDirtyPlaceholder = true

	if err := want.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want a complete record", err)
	}
	if err := j.SetFileRecord(want); err != nil {
		t.Fatal(err)
	}

	got, err := j.FileRecord("a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(want) {
		t.Fatalf("FileRecord =\n %v\nwant\n %v", got, want)
	}

	if got.Etag != "abc123" || got.FileID != "fileid-1" ||
		got.RemotePerm.ToDbValue() != "WDNVCK" || got.ChecksumHeader != "SHA1:deadbeef" {
		t.Errorf("remote fields lost: %v", got)
	}

	byInode, err := j.FileRecordByInode(4242)
	if err != nil || byInode.Path != "a/b.txt" {
		t.Errorf("FileRecordByInode = %v, %v", byInode, err)
	}
	byID, err := j.FileRecordsByFileID("fileid-1")
	if err != nil || len(byID) != 1 || byID[0].Path != "a/b.txt" {
		t.Errorf("FileRecordsByFileID = %v, %v", byID, err)
	}

	kids, err := j.ListFilesInPath("a")
	if err != nil || len(kids) != 1 || kids[0].Path != "a/b.txt" {
		t.Errorf("ListFilesInPath = %v, %v", kids, err)
	}

	if err := j.DeleteRecord("a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if got, err := j.FileRecord("a/b.txt"); err != nil || got.IsValid() {
		t.Errorf("after delete: FileRecord = %v, %v", got, err)
	}
}

func TestAvoidReadFromDbOnNextSync(t *testing.T) {
	j, err := OpenJournal(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()

	dir := NewSyncJournalFileRecord("a", ItemTypeDirectory)
	dir.Etag = "dir-etag"
	dir.Inode, dir.ModTime, dir.HasModTime, dir.FileID = 1, 1, true, "d1"
	dir.RemotePerm = RemotePermissionsFromServerString("RDNVCK")
	if err := j.SetFileRecord(dir); err != nil {
		t.Fatal(err)
	}
	file := NewSyncJournalFileRecord("a/b.txt", ItemTypeFile)
	file.Etag = "file-etag"
	file.Inode, file.ModTime, file.HasModTime, file.FileID = 2, 2, true, "f1"
	file.RemotePerm = RemotePermissionsFromServerString("RDNVW")
	if err := j.SetFileRecord(file); err != nil {
		t.Fatal(err)
	}

	if err := j.AvoidReadFromDbOnNextSync("a/b.txt"); err != nil {
		t.Fatal(err)
	}
	got, err := j.FileRecord("a")
	if err != nil {
		t.Fatal(err)
	}
	if got.Etag != "_invalid_" {
		t.Errorf("parent directory etag = %q, want %q", got.Etag, "_invalid_")
	}
	gotFile, err := j.FileRecord("a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if gotFile.Etag != "file-etag" {
		t.Errorf("file etag = %q, want it untouched (type != 2)", gotFile.Etag)
	}

	if err := j.SetFileRecord(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := j.FileRecord("a"); got.Etag != "_invalid_" {
		t.Errorf("etag written despite the storage filter: %q", got.Etag)
	}

	j.ClearEtagStorageFilter()
	if err := j.SetFileRecord(dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := j.FileRecord("a"); got.Etag != "dir-etag" {
		t.Errorf("after clearing the filter, etag = %q, want %q", got.Etag, "dir-etag")
	}
}

func TestRemotePermissionsRoundTrip(t *testing.T) {
	if got := (RemotePermissions{}); !got.IsNull() || got.ToDbValue() != "" {
		t.Errorf("zero value = %q, want null and empty", got.ToDbValue())
	}
	empty := RemotePermissionsFromServerString("")
	if empty.IsNull() || empty.ToDbValue() != " " {
		t.Errorf("empty server string = %q, want %q and non-null", empty.ToDbValue(), " ")
	}
	if RemotePermissionsFromDbValue("").IsNull() != true {
		t.Error(`FromDbValue("") must be null`)
	}

	perm := RemotePermissionsFromServerString("SRKCVNDW")
	if got := perm.ToDbValue(); got != "WDNVCKRS" {
		t.Errorf("ToDbValue = %q, want %q", got, "WDNVCKRS")
	}
	if !perm.HasPermission(CanWrite) || !perm.HasPermission(IsShared) || perm.HasPermission(IsMounted) {
		t.Errorf("HasPermission wrong for %q", perm)
	}
	if RemotePermissionsFromDbValue(perm.ToDbValue()) != perm {
		t.Error("db round trip changed the value")
	}
}
