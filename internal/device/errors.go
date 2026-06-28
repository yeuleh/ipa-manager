package device

import "errors"

// ErrTunnelRequired indicates an iOS 17+ device needs a tunnel before device
// services can be reached. The CLI surfaces this as an actionable message
// rather than auto-escalating to sudo.
var ErrTunnelRequired = errors.New("ios 17+ tunnel required; run: sudo ios tunnel start")
