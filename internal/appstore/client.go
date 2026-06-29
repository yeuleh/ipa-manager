// Package appstore adapts ipatool's AppStore to per-profile account isolation.
package appstore

// KeychainServiceName is the macOS Keychain service name for ipa-manager.
// Isolated from raw ipatool's "ipatool-auth.service" to avoid keychain item conflicts.
const KeychainServiceName = "ipa-manager"
