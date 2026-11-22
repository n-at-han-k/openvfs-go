package directory

import (
	"strconv"
	"strings"
	"time"

	"example.com/project/src/journal"
)

type RemoteFileData struct {
	name             string
	etag             string
	fileID           string
	checksumHeader   string
	remotePerm       journal.RemotePermissions
	modtime          int64
	size             int64
	isDirectory      bool
	vaultIntegrityID string

	err string
}

const rfc1123Pattern = "Mon, 02 Jan 2006 15:04:05 GMT"

func ParseRFC1123Date(date string) (time.Time, error) {
	if date == "" {
		return time.Time{}, nil
	}
	return time.Parse(rfc1123Pattern, date)
}

func NormalizeEtag(etag string) string {
	if etag == "" {
		return ""
	}
	unQuote := func() {
		if strings.HasPrefix(etag, `"`) && strings.HasSuffix(etag, `"`) {
			etag = strings.TrimSuffix(strings.TrimPrefix(etag, `"`), `"`)
		}
	}

	etag = strings.TrimPrefix(etag, "W/")
	unQuote()
	etag = strings.TrimSuffix(etag, "-gzip")
	unQuote()
	return etag
}

func NewRemoteFileData(fileName string, props map[string]string) RemoteFileData {
	var out RemoteFileData
	var errors []string
	out.name = fileName[strings.LastIndex(fileName, "/")+1:]

	if v, ok := props["resourcetype"]; ok {
		out.isDirectory = strings.Contains(v, "collection")
	}
	if v, ok := props["getlastmodified"]; ok {
		if date, err := ParseRFC1123Date(v); err == nil {
			out.modtime = date.Unix()
		}
	}
	if out.isDirectory {
		out.size = 0
	} else {
		if v, ok := props["getcontentlength"]; ok {
			n, _ := strconv.ParseInt(v, 10, 64)
			out.size = max(0, n)
		} else {
			errors = append(errors, "size")
		}
	}
	if v, ok := props["getetag"]; ok {
		out.etag = NormalizeEtag(v)
	}
	if v, ok := props["id"]; ok {
		out.fileID = v
	}
	if v, ok := props["checksums"]; ok {
		out.checksumHeader = journal.FindBestChecksum(v)
	}
	if v, ok := props["permissions"]; ok {
		out.remotePerm = journal.RemotePermissionsFromServerString(v)
	}
	if v, ok := props["integrity-id"]; ok {
		out.vaultIntegrityID = v
	}

	if out.etag == "" {
		errors = append(errors, "etag")
	}
	if out.fileID == "" {
		errors = append(errors, "id")
	}
	if out.size != 0 && out.checksumHeader == "" {
		errors = append(errors, "checksum")
	}
	if out.remotePerm.IsNull() {
		errors = append(errors, "permissions")
	}
	if len(errors) > 0 {
		out.err = "server reported no " + strings.Join(errors, ", ")
	}
	return out
}

func (i RemoteFileData) Name() string                          { return i.name }
func (i RemoteFileData) IsDirectory() bool                     { return i.isDirectory }
func (i RemoteFileData) Etag() string                          { return i.etag }
func (i RemoteFileData) FileID() string                        { return i.fileID }
func (i RemoteFileData) ChecksumHeader() string                { return i.checksumHeader }
func (i RemoteFileData) RemotePerm() journal.RemotePermissions { return i.remotePerm }
func (i RemoteFileData) ModTime() int64                        { return i.modtime }
func (i RemoteFileData) Size() int64                           { return i.size }
func (i RemoteFileData) Error() string                         { return i.err }
func (i RemoteFileData) IsVaultFile() bool                     { return i.vaultIntegrityID != "" }
func (i RemoteFileData) IsValid() bool                         { return i.name != "" }
