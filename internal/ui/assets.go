package ui

import (
	"embed"
	"net/http"
)

//go:embed static
var assets embed.FS

func Assets() http.Handler { return http.FileServerFS(assets) }
