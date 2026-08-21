package service

import "fmt"

// fmtSscanImpl wraps fmt.Sscan so the service package can parse persisted meta
// values without leaking a bare fmt.Sscan call across the public surface.
func fmtSscanImpl(src string, dst *int) (int, error) {
	return fmt.Sscan(src, dst)
}
