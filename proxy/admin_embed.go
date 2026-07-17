package proxy

import "embed"

//go:embed all:webdist
var adminDist embed.FS
