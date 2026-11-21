// Package protocol holds the protocol-independent HTTP message types.
package protocol

import "net/textproto"

type headerEntry struct {
	key   string // canonical form, e.g. "Content-Type"
	value string
}

// Header is an ordered, case-insensitive multi-map of HTTP header fields.
// Keys are stored in canonical form. The zero value is ready to use.
type Header struct {
	entries []headerEntry
}

// CanonicalKey returns the canonical spelling of a header name.
func CanonicalKey(key string) string {
	return textproto.CanonicalMIMEHeaderKey(key)
}

// Get returns the first value for key, or "" when absent.
func (h *Header) Get(key string) string {
	key = CanonicalKey(key)
	for i := range h.entries {
		if h.entries[i].key == key {
			return h.entries[i].value
		}
	}
	return ""
}

// Values returns every value for key in the order they were added.
func (h *Header) Values(key string) []string {
	key = CanonicalKey(key)
	var values []string
	for i := range h.entries {
		if h.entries[i].key == key {
			values = append(values, h.entries[i].value)
		}
	}
	return values
}

// Has reports whether at least one value exists for key.
func (h *Header) Has(key string) bool {
	key = CanonicalKey(key)
	for i := range h.entries {
		if h.entries[i].key == key {
			return true
		}
	}
	return false
}

// Set replaces every value of key with a single value.
func (h *Header) Set(key, value string) {
	h.Del(key)
	h.Add(key, value)
}

// Add appends a value for key.
func (h *Header) Add(key, value string) {
	h.entries = append(h.entries, headerEntry{key: CanonicalKey(key), value: value})
}

// Del removes every value of key.
func (h *Header) Del(key string) {
	key = CanonicalKey(key)
	kept := h.entries[:0]
	for _, entry := range h.entries {
		if entry.key != key {
			kept = append(kept, entry)
		}
	}
	for i := len(kept); i < len(h.entries); i++ {
		h.entries[i] = headerEntry{} // let the strings be collected
	}
	h.entries = kept
}

// VisitAll calls fn for every field in insertion order.
func (h *Header) VisitAll(fn func(key, value string)) {
	for _, entry := range h.entries {
		fn(entry.key, entry.value)
	}
}

// Len returns the number of fields.
func (h *Header) Len() int { return len(h.entries) }

// Reset removes all fields but keeps the allocated storage.
func (h *Header) Reset() {
	for i := range h.entries {
		h.entries[i] = headerEntry{}
	}
	h.entries = h.entries[:0]
}
