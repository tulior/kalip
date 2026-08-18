package harness

import "regexp"

// regexpMustCompile is a tiny wrapper around regexp.MustCompile
// that panics on bad pattern. It is only called for the two
// patterns defined in types.go which are well-tested.
func regexpMustCompile(pat string) *regexp.Regexp {
	return regexp.MustCompile(pat)
}
