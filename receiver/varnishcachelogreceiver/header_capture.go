package varnishcachelogreceiver

import (
	"sort"
	"strings"
)

// todo: refactor and inline this

// requiredCapturedHeader is captured on every transaction regardless of
// user configuration.
const requiredCapturedHeader = "traceparent"

// capturedHeader binds a lowercase HTTP request header name to the OTel
// span attribute the value should be emitted under. An empty AttrKey
// means "capture but do not emit" — used exclusively for the internally
// required trace-context header.
type capturedHeader struct {
	Name    string
	AttrKey string
}

// buildCapturedHeaders resolves the user's header→attribute map into the
// receiver's shared capture list. Slot 0 is always requiredCapturedHeader
// with an empty AttrKey. User entries are lowercased, trimmed, and sorted
// by header name for deterministic slot ordering; entries with empty
// header or attribute names, and any redundant traceparent entry, are
// silently ignored.
func buildCapturedHeaders(configured map[string]string) []capturedHeader {
	normalized := make(map[string]string, len(configured))
	for k, v := range configured {
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if k == "" || v == "" || k == requiredCapturedHeader {
			continue
		}
		normalized[k] = v
	}

	names := make([]string, 0, len(normalized))
	for k := range normalized {
		names = append(names, k)
	}
	sort.Strings(names)

	result := make([]capturedHeader, 0, len(normalized)+1)
	result = append(result, capturedHeader{Name: requiredCapturedHeader})
	for _, n := range names {
		result = append(result, capturedHeader{Name: n, AttrKey: normalized[n]})
	}
	return result
}
