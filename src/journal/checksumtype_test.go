package journal

import "testing"

func TestFindBestChecksumAndHeader(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ADLER32:1231 SHA1:ab124124 MD5:2131affa21", "SHA1:ab124124"},
		{"MD5:2131affa21 ADLER32:1231", "MD5:2131affa21"},
		{"SHA3-256:abc", "SHA3-256:abc"},
		{"SHA1:ab124124</oc:checksums>", "SHA1:ab124124"},
		{"ADLER32:1231", "ADLER32:1231"},
		{"NOPE:1", ""},
		{"", ""},
	} {
		if got := FindBestChecksum(tc.in); got != tc.want {
			t.Errorf("FindBestChecksum(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	h := ParseChecksumHeader("SHA1:deadbeef")
	if !h.IsValid() || h.Type() != ChecksumSHA1 || h.Checksum() != "deadbeef" {
		t.Errorf("ParseChecksumHeader = %+v", h)
	}
	if got := h.MakeChecksumHeader(); got != "SHA1:deadbeef" {
		t.Errorf("MakeChecksumHeader = %q", got)
	}
	if bad := ParseChecksumHeader("garbage"); bad.IsValid() || bad.Error() == "" {
		t.Errorf("malformed header accepted: %+v", bad)
	}
	if bad := ParseChecksumHeader("WHIRLPOOL:ab"); bad.IsValid() || bad.Type() != ChecksumParseError {
		t.Errorf("unknown type accepted: %+v", bad)
	}
}
