package openvfs

import (
	"reflect"
	"testing"
)

// Transpiled from src/openvfsfuse/tests/strtools_test.cpp.
//
// Minimal unit tests for Join and Split. Each failed check reports a
// message and the test fails, which `go test` interprets as a test failure.

func TestJoinBasic(t *testing.T) {
	input := []string{"a", "b", "c"}
	check(t, Join(input, ',') == "a,b,c", "join basic three elements")
}

func TestJoinSingleElement(t *testing.T) {
	input := []string{"only"}
	check(t, Join(input, ',') == "only", "join single element")
}

func TestJoinEmptyVector(t *testing.T) {
	input := []string{}
	check(t, Join(input, ',') == "", "join empty vector yields empty string")
}

func TestJoinWithEmptyStrings(t *testing.T) {
	input := []string{"", "b", ""}
	check(t, Join(input, ',') == ",b,", "join preserves empty elements")
}

func TestSplitBasic(t *testing.T) {
	expected := []string{"a", "b", "c"}
	check(t, reflect.DeepEqual(Split("a,b,c", ','), expected), "split basic three elements")
}

func TestSplitNoDelimiter(t *testing.T) {
	expected := []string{"abc"}
	check(t, reflect.DeepEqual(Split("abc", ','), expected), "split with no delimiter present")
}

func TestSplitEmptyString(t *testing.T) {
	expected := []string{""}
	check(t, reflect.DeepEqual(Split("", ','), expected), "split of empty string yields one empty token")
}

func TestSplitConsecutiveDelimiters(t *testing.T) {
	expected := []string{"a", "", "b"}
	check(t, reflect.DeepEqual(Split("a,,b", ','), expected), "split with consecutive delimiters yields empty token")
}

func TestSplitLeadingAndTrailingDelimiter(t *testing.T) {
	expected := []string{"", "a", "b", ""}
	check(t, reflect.DeepEqual(Split(",a,b,", ','), expected), "split with leading and trailing delimiter")
}

func TestJoinSplitRoundTrip(t *testing.T) {
	input := []string{"one", "two", "three"}
	joined := Join(input, '/')
	splitBack := Split(joined, '/')
	check(t, reflect.DeepEqual(splitBack, input), "join followed by split round-trips to original vector")
}

func check(t *testing.T, condition bool, message string) {
	t.Helper()
	if !condition {
		t.Errorf("FAILED: %s", message)
	}
}
