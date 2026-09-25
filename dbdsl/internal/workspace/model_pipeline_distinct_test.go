package workspace

import (
	"strings"
	"testing"
)

func TestDistinctStringsKeepsFirstOccurrence(t *testing.T) {
	got := distinctStrings([]string{"a", "b"}, []string{"b", "c"}, []string{"a", "c", "d"})
	if strings.Join(got, ",") != "a,b,c,d" {
		t.Fatalf("unexpected warnings: %v", got)
	}
}
