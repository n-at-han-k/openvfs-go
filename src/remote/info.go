// Package remote fetches the remote state of a path via WebDAV PROPFIND.
// The three inputs discovery compares are:
//
//  1. directory.Meta — state from the last successful sync (journal)
//  2. RemoteInfo     — entry from PROPFIND
//  3. LocalInfo      — entry from the local filesystem
package remote

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

type RemoteInfo struct {
	Etag        string
	FileID      string
	Size        int64
	ModTime     int64
	Permissions string
	Checksum    string
	IsDir       bool
}

type LocalInfo struct {
	Size    int64
	ModTime int64
	Inode   uint64
	Mode    os.FileMode
}

type davMultistatus struct {
	XMLName  xml.Name      `xml:"multistatus"`
	Response []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string        `xml:"href"`
	Propstat []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Status string  `xml:"status"`
	Prop   davProp `xml:"prop"`
}

type davProp struct {
	Etag          string `xml:"getetag"`
	LastModified  string `xml:"getlastmodified"`
	ContentLength string `xml:"getcontentlength"`
	ResourceType  struct {
		Collection *struct{} `xml:"collection"`
	} `xml:"resourcetype"`
	FileID      string `xml:"http://owncloud.org/ns fileid"`
	Permissions string `xml:"http://owncloud.org/ns permissions"`
	Checksums   struct {
		Checksum string `xml:"http://owncloud.org/ns checksum"`
	} `xml:"http://owncloud.org/ns checksums"`
}

// Fetch reads a path's remote entry with a depth-0 PROPFIND.
func Fetch(ctx context.Context, client *http.Client, url string) (RemoteInfo, error) {
	var out RemoteInfo
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", url, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Depth", "0")
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return out, fmt.Errorf("PROPFIND %s: %s: %s", url, resp.Status, body)
	}

	var ms davMultistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return out, err
	}
	for _, r := range ms.Response {
		for _, ps := range r.Propstat {
			p := ps.Prop
			out.Etag = p.Etag
			out.FileID = p.FileID
			out.Permissions = p.Permissions
			out.Checksum = p.Checksums.Checksum
			out.IsDir = p.ResourceType.Collection != nil
			if p.ContentLength != "" {
				out.Size, _ = strconv.ParseInt(p.ContentLength, 10, 64)
			}
			if p.LastModified != "" {
				if t, err := http.ParseTime(p.LastModified); err == nil {
					out.ModTime = t.UnixNano()
				}
			}
			return out, nil
		}
	}
	return out, fmt.Errorf("PROPFIND %s: no properties in response", url)
}
