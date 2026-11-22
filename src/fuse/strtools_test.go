package fuse

import (
	"reflect"
	"testing"
)

// Transpiled from src/openvfsfuse/tests/strtools_test.cpp.
//
// Minimal unit tests for strJoin and strSplit. Each failed check reports a
// message and the test fails, which `go test` interprets as a test failure.

func TestJoinBasic(t *testing.T) {
	input := []string{"a", "b", "c"}
	check(t, strJoin(input, ',') == "a,b,c", "join basic three elements")
}

func TestJoinSingleElement(t *testing.T) {
	input := []string{"only"}
	check(t, strJoin(input, ',') == "only", "join single element")
}

func TestJoinEmptyVector(t *testing.T) {
	input := []string{}
	check(t, strJoin(input, ',') == "", "join empty vector yields empty string")
}

func TestJoinWithEmptyStrings(t *testing.T) {
	input := []string{"", "b", ""}
	check(t, strJoin(input, ',') == ",b,", "join preserves empty elements")
}

func TestSplitBasic(t *testing.T) {
	expected := []string{"a", "b", "c"}
	check(t, reflect.DeepEqual(strSplit("a,b,c", ','), expected), "split basic three elements")
}

func TestSplitNoDelimiter(t *testing.T) {
	expected := []string{"abc"}
	check(t, reflect.DeepEqual(strSplit("abc", ','), expected), "split with no delimiter present")
}

func TestSplitEmptyString(t *testing.T) {
	expected := []string{""}
	check(t, reflect.DeepEqual(strSplit("", ','), expected), "split of empty string yields one empty token")
}

func TestSplitConsecutiveDelimiters(t *testing.T) {
	expected := []string{"a", "", "b"}
	check(t, reflect.DeepEqual(strSplit("a,,b", ','), expected), "split with consecutive delimiters yields empty token")
}

func TestSplitLeadingAndTrailingDelimiter(t *testing.T) {
	expected := []string{"", "a", "b", ""}
	check(t, reflect.DeepEqual(strSplit(",a,b,", ','), expected), "split with leading and trailing delimiter")
}

func TestJoinSplitRoundTrip(t *testing.T) {
	input := []string{"one", "two", "three"}
	joined := strJoin(input, '/')
	splitBack := strSplit(joined, '/')
	check(t, reflect.DeepEqual(splitBack, input), "join followed by split round-trips to original vector")
}

func check(t *testing.T, condition bool, message string) {
	t.Helper()
	if !condition {
		t.Errorf("FAILED: %s", message)
	}
}
