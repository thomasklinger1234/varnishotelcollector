package spanname

import (
	"net/url"
	"regexp"
	"strings"
)

// InvalidURLPlaceholder is returned when the input cannot be parsed as a URL.
const InvalidURLPlaceholder = "<invalid-url>"

// maxSegments caps the number of path segments considered. Anything past this
// is dropped.
const maxSegments = 8

var (
	// uuidPattern matches canonical 8-4-4-4-12 UUIDs (v1-v5, any case).
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

	// hashPattern matches fixed-length hex strings commonly used for content
	hashPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{32}|[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

	// intPattern matches decimal integers with length >= 2 (so single-digit
	// version-like segments such as "v1" survive untouched via the surrounding
	// literal).
	intPattern = regexp.MustCompile(`^[1-9][0-9]+$`)

	// filenamePattern matches "<name>.<ext>" where ext is one of the allowlisted
	// web asset extensions.
	filenamePattern = regexp.MustCompile(`^[^/]+\.(?:js|mjs|cjs|css|html|htm|png|jpg|jpeg|gif|svg|webp|avif|ico|json|xml|woff|woff2|ttf|otf|eot|map|txt|pdf|mp4|mp3|wav|ogg|webm|zip|gz|br|wasm)$`)

	// tokenPattern matches long opaque strings (>=20 chars).
	tokenPattern = regexp.MustCompile(`^(?:.{20,}|.*[%&'()*+,:=@].*)$`)
)

// Normalize replaces high-cardinality path segments of rawURL with named
// placeholders and returns the resulting low-cardinality path without the querystring.
//
// Classification rules, first match wins per segment:
//
//  1. UUID 					              -> ":uuid"
//  2. Fixed-length hex hash (32/40/64)       -> ":hash"
//  3. Multi-digit integer                    -> ":id"
//  4. Filename with allowlisted extension    -> ":filename"
//  5. Long/URL-encoded-shaped opaque string  -> ":token"
//  6. otherwise                              -> segment kept verbatim
func Normalize(rawURL string) string {
	if rawURL == "" {
		return "/"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil {
		return InvalidURLPlaceholder
	}
	path := parsed.EscapedPath()
	if path == "" {
		return "/"
	}

	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	if len(segments) == 0 {
		return "/"
	}
	truncated := len(segments) > maxSegments
	if truncated {
		segments = segments[:maxSegments]
	}
	for i, seg := range segments {
		segments[i] = classify(seg)
	}
	result := "/" + strings.Join(segments, "/")
	if !truncated && strings.HasSuffix(path, "/") {
		result += "/"
	}
	return result
}

// classify applies the rule table to a single non-empty path segment.
func classify(seg string) string {
	switch {
	case uuidPattern.MatchString(seg):
		return ":uuid"
	case hashPattern.MatchString(seg):
		return ":hash"
	case intPattern.MatchString(seg):
		return ":id"
	case filenamePattern.MatchString(seg):
		return ":filename"
	case tokenPattern.MatchString(seg):
		return ":token"
	default:
		return seg
	}
}
