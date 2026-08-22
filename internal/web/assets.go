package web

import (
	"embed"
	"io/fs"
)

//go:embed static/*
var assets embed.FS

func Assets() fs.FS {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
