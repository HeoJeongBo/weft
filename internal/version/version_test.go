package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := String()
	wants := []string{
		"weft",
		Version,
		Commit,
		Date,
		runtime.Version(),
		runtime.GOOS,
		runtime.GOARCH,
	}
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("String() = %q, want it to contain %q", s, w)
		}
	}
}
