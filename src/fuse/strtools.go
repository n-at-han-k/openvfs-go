package fuse

import "strings"

// Transpiled from src/openvfsfuse/strtools.{h,cpp}.

// strJoin takes a slice of strings and returns a string of all entries of the
// slice joined together, delimited by the joiner character.
func strJoin(v []string, joiner byte) string {
	return strings.Join(v, string(joiner))
}

// strSplit takes a string, splits it at all occurances of delimiter and returns
// a slice of strings containing all parts without the delimiter.
func strSplit(str string, delimiter byte) []string {
	return strings.Split(str, string(delimiter))
}
