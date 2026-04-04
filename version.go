package main

// Version is set at build time via ldflags:
//
//	go build -ldflags="-X main.Version=1.0.0" .
//
// Falls back to "dev" for development builds.
var Version = "dev"
