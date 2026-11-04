// Package web embeds the independently implemented device client.
package web

import "embed"

//go:embed static/*
var Files embed.FS
