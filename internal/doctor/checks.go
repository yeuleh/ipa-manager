// Package doctor runs environment health checks.
package doctor

import "github.com/yeuleh/ipa-manager/internal/apperr"

// Result is a single check outcome.
type Result struct {
	Name   string
	OK     bool
	Detail string
}

// Run executes all health checks.
//
// v1 checks: Go/runtime version, macOS, keychain backend opens, go-ios can
// list devices, and iOS 17+ tunnel guidance. doctor never auto-escalates sudo.
func Run() ([]Result, error) { return nil, apperr.ErrNotImplemented }
