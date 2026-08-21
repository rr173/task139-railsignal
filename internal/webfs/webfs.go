// Package webfs embeds the native HTML/CSS/JS frontend so the engine serves a
// single binary with no external file dependencies. The frontend is a plain
// operator console: build the yard, place points/signals, request routes,
// drive a train through (occupancy/clearance), and watch the interlocking
// state and section-by-section release update in real time.
package webfs

import "embed"

//go:embed web/index.html web/app.js web/style.css
var files embed.FS

// FS returns the embedded filesystem.
func FS() embed.FS { return files }
