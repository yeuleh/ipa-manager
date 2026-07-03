package device

// (Sentinels formerly here — ErrTunnelRequired — were removed after live testing
// on iOS 26 showed install/apps/uninstall work over usbmuxd WITHOUT a tunnel,
// invalidating the "iOS 17+ needs tunnel" premise. See docs/features/ios-device-manage
// design DD-02 history / plan.md ledger. Device-specific error sentinels
// (ErrAppNotInstalled etc.) live in internal/apperr.)
