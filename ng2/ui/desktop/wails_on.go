//go:build windows || wails

package desktop

import (
	"context"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"io/fs"
)

// Fenêtre native (Wails v2 : WebView2 sous Windows, WebKitGTK sous Linux avec
// la balise « wails »). Wails exige aussi ses balises de build « desktop,production »
// (ou « dev ») : sans elles, wails.Run rend une erreur explicite.
const wailsAvailable = true

func runWails() error {
	b := &Bridge{}
	var ctx context.Context
	b.app = NewApp(func(name string, data any) {
		if ctx != nil {
			wrt.EventsEmit(ctx, "ev", map[string]any{"name": name, "data": data})
		}
	})
	assets, err := fs.Sub(frontendFS, "frontend")
	if err != nil {
		return err
	}
	return wails.Run(&options.App{
		Title:            "Claude Launcher",
		Width:            1440,
		Height:           900,
		MinWidth:         900,
		MinHeight:        560,
		BackgroundColour: &options.RGBA{R: 15, G: 18, B: 23, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup: func(c context.Context) {
			ctx = c
			quitHook = func() { wrt.Quit(c) }
			themeHook = func(mode string) { windowTheme(c, mode) }
			b.app.Start()
		},
		OnShutdown: func(context.Context) { b.app.Stop() },
		Bind:       []any{b},
		Windows:    &windows.Options{Theme: windows.SystemDefault, WebviewIsTransparent: false, WindowIsTranslucent: false},
	})
}

// windowTheme : barre de titre de la fenêtre au thème choisi (#133).
func windowTheme(ctx context.Context, mode string) {
	switch mode {
	case "light":
		wrt.WindowSetLightTheme(ctx)
	case "dark":
		wrt.WindowSetDarkTheme(ctx)
	default:
		wrt.WindowSetSystemDefaultTheme(ctx)
	}
}
