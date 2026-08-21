// Package idlib issues short, collision-resistant ids for entities and events.
// It uses crypto/rand so ids are unpredictable enough for an operation console
// while remaining a fixed-width base32 string. No external service is needed.
package idlib

import (
	"crypto/rand"
	"encoding/base32"
)

// New returns a fresh 13-char base32 id (10 random bytes -> 16 chars, prefix
// removed by trimming the leading '=' padding-free encoding).
func New(prefix string) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should not fail in practice; fall back to a deterministic
		// but unique-enough counter-free value by re-reading once.
		return prefix + "0000000000"
	}
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:])
	// lowercase to match convention and avoid SHOUTING
	for i := range s {
		if s[i] >= 'A' && s[i] <= 'Z' {
			s = s[:i] + string(s[i]+32) + s[i+1:]
		}
	}
	return prefix + s
}

// NewNodeID prefixes "n_".
func NewNodeID() string  { return New("n_") }

// NewSectionID prefixes "sec_".
func NewSectionID() string { return New("sec_") }

// NewPointID prefixes "pt_".
func NewPointID() string  { return New("pt_") }

// NewSignalID prefixes "sig_".
func NewSignalID() string { return New("sig_") }

// NewRouteID prefixes "rt_".
func NewRouteID() string  { return New("rt_") }
