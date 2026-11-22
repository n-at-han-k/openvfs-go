package directory

import (
	"strings"
	"testing"
)

func TestNormalizeEtag(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"abc"`, "abc"},
		{`W/"abc"`, "abc"},
		{`"abc-gzip"`, "abc"},
		{`W/"abc-gzip"`, "abc"},
		{"abc", "abc"},
		{"", ""},
	} {
		if got := NormalizeEtag(tc.in); got != tc.want {
			t.Errorf("NormalizeEtag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewRemoteFileData(t *testing.T) {
	info := NewRemoteFileData("/remote.php/dav/files/a/b.txt", map[string]string{
		"getetag":          `"abc123"`,
		"id":               "fileid-1",
		"getcontentlength": "99",
		"getlastmodified":  "Tue, 14 Nov 2023 22:13:20 GMT",
		"checksums":        "ADLER32:1231 SHA1:deadbeef",
		"permissions":      "RDNVW",
		"resourcetype":     "",
	})
	if !info.IsValid() || info.Name() != "b.txt" {
		t.Fatalf("Name = %q, valid = %t", info.Name(), info.IsValid())
	}
	if info.Etag() != "abc123" || info.FileID() != "fileid-1" || info.Size() != 99 {
		t.Errorf("info = %+v", info)
	}
	if info.ChecksumHeader() != "SHA1:deadbeef" {
		t.Errorf("ChecksumHeader = %q", info.ChecksumHeader())
	}
	if info.ModTime() != 1700000000 {
		t.Errorf("ModTime = %d, want 1700000000", info.ModTime())
	}
	if info.IsDirectory() || info.Error() != "" {
		t.Errorf("IsDirectory = %t, Error = %q", info.IsDirectory(), info.Error())
	}

	dir := NewRemoteFileData("a", map[string]string{
		"resourcetype": "<d:collection/>", "getetag": `"e"`, "id": "x", "permissions": "RDNVCK",
	})
	if !dir.IsDirectory() || dir.Size() != 0 || dir.Error() != "" {
		t.Errorf("dir = %+v, error = %q", dir, dir.Error())
	}

	bad := NewRemoteFileData("a/b.txt", map[string]string{"getcontentlength": "5"})
	for _, want := range []string{"etag", "id", "checksum", "permissions"} {
		if !strings.Contains(bad.Error(), want) {
			t.Errorf("Error = %q, want it to mention %q", bad.Error(), want)
		}
	}

	neg := NewRemoteFileData("a/b.txt", map[string]string{"getcontentlength": "-1"})
	if neg.Size() != 0 {
		t.Errorf("Size = %d, want 0", neg.Size())
	}
}
