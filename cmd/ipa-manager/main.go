// Package main is the ipa-manager CLI entry point.
package main

import "github.com/yeuleh/ipa-manager/internal/cli"

// Version is overridden at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	cli.Execute(Version)
}
