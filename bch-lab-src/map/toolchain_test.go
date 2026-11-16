package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The Go that builds the released binary is the one in the Dockerfile; the one
// CI tests with comes from go.mod. Both have to name the same release, and the
// build image has to be pinned by digest like every other image this project
// ships, or a tag moved by somebody else decides what gets compiled.
func TestBuildImageMatchesGoModAndIsPinned(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go (\d+\.\d+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod has no go directive")
	}
	want := string(m[1])

	df, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	from := regexp.MustCompile(`(?m)^FROM .*golang:(\S+)`).FindSubmatch(df)
	if from == nil {
		t.Fatal("Dockerfile has no golang build stage")
	}
	ref := string(from[1])
	if !strings.HasPrefix(ref, want+".") && !strings.HasPrefix(ref, want+"-") {
		t.Errorf("Dockerfile builds with golang:%s, go.mod says go %s", ref, want)
	}
	if !regexp.MustCompile(`@sha256:[0-9a-f]{64}$`).MatchString(ref) {
		t.Errorf("golang:%s is not pinned by digest", ref)
	}
}
