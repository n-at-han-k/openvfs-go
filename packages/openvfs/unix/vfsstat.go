//go:build linux || darwin

package unix

import (
	"example.com/project/packages/openvfs"

	"fmt"
	"os"
	"path/filepath"
)

// Transpiled from src/vfsstat/main.cpp.

func getPlaceholderAttribs(path string) PlaceHolderAttributes {
	data, ok := xattrGetString(path, openvfs.XAttrData)
	if !ok {
		return PlaceHolderFromData(path, nil)
	}
	return PlaceHolderFromData(path, []byte(data))
}

func statEntry(path string) {
	data := getPlaceholderAttribs(path)
	valid := "❌"
	if data.Validate() {
		valid = "✅"
	}
	fmt.Println(formatRow(filepath.Base(path), valid,
		fmt.Sprint(data.Size), fmt.Sprint(data.RealSize()),
		data.State.String(), data.PinState.String(), data.Etag, data.FileID))
}

// formatRow is the std::format call shared by the header and the rows:
// "{:<60} {:^4} {:>10} {:>10} {:>10} {:>10} {:<33} {}" for a row, with the
// header's left aligned variant produced by headerRow below.
func formatRow(path, valid, size, actual, state, pinState, etag, fileID string) string {
	return fmt.Sprintf("%-60s %s %10s %10s %10s %10s %-33s %s",
		path, center(valid, 4), size, actual, state, pinState, etag, fileID)
}

// center is std::format's '^' alignment.
func center(s string, width int) string {
	n := len([]rune(s))
	if n >= width {
		return s
	}
	left := (width - n) / 2
	right := width - n - left
	return spaces(left) + s + spaces(right)
}

func spaces(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}

// RunStat is main() of src/vfsstat/main.cpp. It returns the process exit code,
// which upstream leaves at -1 for everything but a directory listing.
func RunStat(argv []string) int {
	if !platformInstalled() {
		fmt.Fprintln(os.Stderr, errNoPlatform)
		return -1
	}
	if len(argv) == 2 {
		path := argv[1]
		info, err := os.Lstat(path)
		if err != nil {
			fmt.Printf("%q does not exist\n", path)
			return -1
		}
		if registration := RegistrationFromAttributes(path); registration.IsOK() {
			fmt.Printf("OpenVfs Stat: '%s' Owner: '%s' OpenVFS Version: %d\n", path, registration.Owner, registration.Version)
		} else {
			fmt.Printf("OpenVfs Stat: '%s' (%s)\n", path, registration.Error)
		}
		fmt.Printf("%-60s %-4s %-10s %-10s %-10s %-10s %-33s %s\n",
			"Path", "Valid", "Size", "ActualSize", "State", "PinState", "Etag", "FileId")
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				return -1
			}
			for _, child := range entries {
				statEntry(filepath.Join(path, child.Name()))
			}
			return 0
		}
		statEntry(path)
	}
	return -1
}
