package spanname

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Edge cases
		{"empty string", "", "/"},
		{"root", "/", "/"},
		{"only host", "http://example.com", "/"},
		{"only host with slash", "http://example.com/", "/"},
		{"query only", "/?q=x", "/"},
		{"fragment only", "/#top", "/"},

		// Literal segments preserved
		{"single literal", "/users", "/users"},
		{"multi literal", "/api/v1/users", "/api/v1/users"},
		{"version-like segment kept", "/api/v2/health", "/api/v2/health"},
		{"slug preserved", "/users/johndoe", "/users/johndoe"},
		{"decimal version segment kept", "/api/v1.0/x", "/api/v1.0/x"},

		// Rule 1: UUID
		{"uuid lowercase", "/users/550e8400-e29b-41d4-a716-446655440000", "/users/:uuid"},
		{"uuid uppercase", "/USERS/550E8400-E29B-41D4-A716-446655440000", "/USERS/:uuid"},
		{"uuid mid path", "/users/550e8400-e29b-41d4-a716-446655440000/posts", "/users/:uuid/posts"},

		// Rule 2: hash (MD5/SHA-1/SHA-256)
		{"md5 hash", "/cache/5d41402abc4b2a76b9719d911017c592", "/cache/:hash"},
		{"sha1 hash", "/cache/aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d", "/cache/:hash"},
		{"sha256 hash", "/cache/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "/cache/:hash"},

		// Rule 3: multi-digit integer
		{"long numeric id", "/foo/bar/123389258238582", "/foo/bar/:id"},
		{"two-digit id", "/orders/42", "/orders/:id"},
		{"single digit stays literal", "/orders/5", "/orders/5"},

		// Rule 4: filename with allowlisted extension
		{"js filename", "/assets/app.js", "/assets/:filename"},
		{"js filename with query", "/foo/bar/test.js?version=bla", "/foo/bar/:filename"},
		{"css filename", "/static/main.css", "/static/:filename"},
		{"png image", "/img/logo.png", "/img/:filename"},
		{"woff2 font", "/fonts/inter.woff2", "/fonts/:filename"},
		{"source map", "/assets/app.js.map", "/assets/:filename"},
		{"custom extension NOT allowlisted", "/data/report.xyz", "/data/report.xyz"},

		// Rule 5: opaque token
		{"long opaque token", "/auth/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9", "/auth/:token"},
		{"url-encoded chars", "/session/abc%20def", "/session/:token"},

		// Combined
		{"multiple placeholders", "/api/v1/users/12345/orders/550e8400-e29b-41d4-a716-446655440000", "/api/v1/users/:id/orders/:uuid"},

		// Trailing slash
		{"trailing slash preserved", "/users/", "/users/"},
		{"trailing slash with id", "/users/42/", "/users/:id/"},

		// Query and fragment dropped
		{"query dropped", "/users?id=1&name=x", "/users"},
		{"fragment dropped", "/users#profile", "/users"},

		// Cap on segment count
		{"segment cap", "/a/b/c/d/e/f/g/h/i/j/k", "/a/b/c/d/e/f/g/h"},

		// Collapsed double slashes
		{"double slash collapsed", "/foo//bar", "/foo/bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalize_InvalidURL(t *testing.T) {
	// url.Parse is very permissive; construct an input it actually rejects.
	// Control characters in the URL are rejected by net/url.
	got := Normalize("http://exa\x7fmple.com/foo")
	assert.Equal(t, InvalidURLPlaceholder, got)
}

func BenchmarkNormalize(b *testing.B) {
	inputs := []string{
		"/api/v1/users/12345/orders/550e8400-e29b-41d4-a716-446655440000",
		"/assets/app.js?v=1",
		"/cache/5d41402abc4b2a76b9719d911017c592",
		"/health",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Normalize(inputs[i%len(inputs)])
	}
}
