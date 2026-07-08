package domain

import (
	"regexp"
	"strings"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidName reports whether s is a valid session name.
func ValidName(s string) bool {
	return s != "" && nameRE.MatchString(s)
}

// Slugify lowercases s and collapses runs of non-alphanumeric characters into a
// single dash, trimming leading/trailing dashes. Used for project slugs.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// SessionKey is the "<project>/<name>" correlation key stamped into docker labels.
func SessionKey(projectSlug, name string) string {
	return projectSlug + "/" + name
}

// SplitSessionKey splits a "<project>/<name>" key.
func SplitSessionKey(key string) (project, name string, ok bool) {
	return strings.Cut(key, "/")
}
