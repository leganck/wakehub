package main

// Windows VERSIONINFO for wakehub-client.exe (ProductName / FileDescription).
// Regenerates rsrc_windows_amd64.syso (only linked for windows/amd64).
//
//	go generate ./cmd/client
//
//go:generate go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 -64 -o rsrc_windows_amd64.syso versioninfo.json
