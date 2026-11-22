package journal

import (
	"fmt"
	"strings"
)

const createChecksumTypeTable = `CREATE TABLE IF NOT EXISTS checksumtype(
id INTEGER PRIMARY KEY,
name TEXT UNIQUE
);`

func (j *Journal) mapChecksumType(checksumType ChecksumAlgorithm) int {
	if checksumType == ChecksumParseError || checksumType == ChecksumNone {
		return 0
	}
	if id, ok := j.checksumTypeCache[checksumType]; ok {
		return id
	}

	typeName := checksumType.String()

	if _, err := j.db.Exec("INSERT OR IGNORE INTO checksumtype (name) VALUES (?1)", typeName); err != nil {
		return 0
	}

	var id int
	if err := j.db.QueryRow("SELECT id FROM checksumtype WHERE UPPER(name)=?1", typeName).Scan(&id); err != nil {
		return 0
	}
	j.checksumTypeCache[checksumType] = id
	return id
}

type ChecksumAlgorithm int

const (
	ChecksumNone          ChecksumAlgorithm = 0
	ChecksumSHA3_256      ChecksumAlgorithm = 12
	ChecksumSHA256        ChecksumAlgorithm = 4
	ChecksumSHA1          ChecksumAlgorithm = 2
	ChecksumMD5           ChecksumAlgorithm = 1
	ChecksumADLER32       ChecksumAlgorithm = 100
	ChecksumDummyForTests ChecksumAlgorithm = 101
	ChecksumParseError    ChecksumAlgorithm = 102
)

var checksumAlgorithmsAll = []ChecksumAlgorithm{
	ChecksumSHA3_256, ChecksumSHA256, ChecksumSHA1, ChecksumMD5, ChecksumADLER32, ChecksumDummyForTests,
}

func (a ChecksumAlgorithm) String() string {
	switch a {
	case ChecksumSHA3_256:
		return "SHA3-256"
	case ChecksumSHA256:
		return "SHA256"
	case ChecksumSHA1:
		return "SHA1"
	case ChecksumMD5:
		return "MD5"
	case ChecksumADLER32:
		return "ADLER32"
	case ChecksumDummyForTests:
		return "DUMMY_FOR_TESTS"
	}
	return "ERROR"
}

func ChecksumAlgorithmFromName(s string) ChecksumAlgorithm {
	for _, a := range checksumAlgorithmsAll {
		if a.String() == s {
			return a
		}
	}
	return ChecksumParseError
}

func ChecksumAlgorithmFromString(s string) ChecksumAlgorithm {
	return ChecksumAlgorithmFromName(strings.ToUpper(s))
}

func FindBestChecksum(checksums string) string {
	if checksums == "" {
		return ""
	}

	upper := strings.ToUpper(checksums)
	i := -1
	for _, algo := range checksumAlgorithmsAll {
		if at := strings.Index(upper, algo.String()); at != -1 {
			i = at
			break
		}
	}
	if i == -1 {
		return ""
	}

	end := strings.IndexByte(checksums[i:], ' ')
	if end == -1 {
		end = strings.IndexByte(checksums[i:], '<')
	}
	if end == -1 {
		return checksums[i:]
	}
	return checksums[i : i+end]
}

type ChecksumHeader struct {
	checksumType ChecksumAlgorithm
	checksum     string
	err          string
}

func ParseChecksumHeader(header string) ChecksumHeader {
	if header == "" {
		return ChecksumHeader{}
	}
	var out ChecksumHeader
	idx := strings.IndexByte(header, ':')
	if idx < 0 {
		out.err = fmt.Sprintf("The checksum header is malformed: %s", header)
	} else {
		out.checksumType = ChecksumAlgorithmFromString(header[:idx])
		if out.checksumType == ChecksumParseError {
			out.err = fmt.Sprintf("The checksum header contained an unknown checksum type %q", header[:idx])
		}
		out.checksum = header[idx+1:]
	}
	return out
}

func NewChecksumHeader(checksumType ChecksumAlgorithm, checksum string) ChecksumHeader {
	return ChecksumHeader{checksumType: checksumType, checksum: checksum}
}

func (c ChecksumHeader) MakeChecksumHeader() string {
	if !c.IsValid() {
		return ""
	}
	return c.checksumType.String() + ":" + c.checksum
}

func (c ChecksumHeader) Type() ChecksumAlgorithm { return c.checksumType }
func (c ChecksumHeader) Checksum() string        { return c.checksum }
func (c ChecksumHeader) Error() string           { return c.err }

func (c ChecksumHeader) IsValid() bool {
	return c.checksumType != ChecksumNone && c.checksumType != ChecksumParseError && c.checksum != ""
}
