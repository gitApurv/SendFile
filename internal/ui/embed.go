package ui

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var staticFiles embed.FS

func GetFS() fs.FS {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
