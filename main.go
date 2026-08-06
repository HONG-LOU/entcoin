package main

import (
	"embed"
	"log"
	"os"
	"strings"

	"github.com/HONG-LOU/entcoin/entpay"
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
	initial := make([]entpay.LaunchRequest, 0, 1)
	invalidLaunch := false
	for _, argument := range os.Args[1:] {
		if launch, parseErr := entpay.ParseLaunchURL(argument); parseErr == nil && launch.Handoff == "" {
			initial = append(initial, launch)
		} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(argument)), "entcoin:") {
			invalidLaunch = true
		}
	}
	app := NewApp(initial...)
	if invalidLaunch {
		app.recordInvalidEntPayLaunch()
	}
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
			OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
				for _, argument := range data.Args {
					if launch, parseErr := entpay.ParseLaunchURL(argument); parseErr == nil && launch.Handoff == "" {
						app.routeSystemLaunch(launch)
					} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(argument)), "entcoin:") {
						app.recordInvalidEntPayLaunch()
					}
				}
				app.focusWindow()
			},
		},
		Bind: []interface{}{app},
	})
	if err != nil {
		log.Fatal(err)
	}
}
