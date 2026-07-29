package main

import (
	"embed"
	"log"
	"os"

	"github.com/HONG-LOU/entcoin/internal/updater"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if handled, err := updater.HandleUpdateHelper(os.Args[1:]); handled {
		if err != nil {
			log.Printf("apply Entcoin update: %v", err)
		}
		return
	}
	app := NewApp()
	err := wails.Run(&options.App{
		Title:             "Entcoin",
		Width:             1180,
		Height:            780,
		MinWidth:          860,
		MinHeight:         640,
		DisableResize:     false,
		Fullscreen:        false,
		Frameless:         false,
		StartHidden:       false,
		HideWindowOnClose: false,
		BackgroundColour:  &options.RGBA{R: 244, G: 245, B: 242, A: 1},
		AssetServer:       &assetserver.Options{Assets: assets},
		OnStartup:         app.startup,
		OnShutdown:        app.shutdown,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "d959ac6b-bfbc-478f-9aa1-43b06a52f76b",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				app.focusWindow()
			},
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
