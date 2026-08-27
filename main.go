package main

import (
	"embed"
	"os"

	"oneclick-dev-server/internal/datastore"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if handled, exitCode := datastore.RunElevatedHelper(os.Args); handled {
		os.Exit(exitCode)
	}
	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "OneClick Dev Server",
		Width:     1180,
		Height:    760,
		MinWidth:  920,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 244, G: 247, B: 251, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
