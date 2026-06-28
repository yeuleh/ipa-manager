// Package apperr defines shared sentinel errors used across packages.
package apperr

import "errors"

// ErrNotImplemented marks a stubbed operation not yet implemented.
// Scaffolded commands return this until their feature mission lands.
var ErrNotImplemented = errors.New("not implemented")
