package appstore

import ipaappstore "github.com/majd/ipatool/v2/pkg/appstore"

// ErrAuthCodeRequired indicates 2FA is required for the in-progress login.
// Aliases ipatool's sentinel so CLI callers can errors.Is against it and
// retry Login with an AuthCode collected via ui.InputAuthCode.
var ErrAuthCodeRequired = ipaappstore.ErrAuthCodeRequired
