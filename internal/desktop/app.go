//go:build desktop

package desktop

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/devenjarvis/docket/internal/app"
	"github.com/devenjarvis/docket/internal/loginenv"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed appicon.png
var appIcon []byte

const (
	mainWindowName          = "main"
	mainWidth, mainHeight   = 1320, 900
	minWidth, minHeight     = 720, 520
	notifyLabel             = "Open"
	notifyPermissionMessage = "system notifications are off, so Docket cannot tell you when a pull request is ready"
)

// film is the design system's whiteprint --film, shown before first paint so
// the window does not flash white.
var film = application.NewRGB(0xE9, 0xEC, 0xE6)

// Available reports whether this binary was built with the desktop app. It
// was.
func Available() bool { return true }

// Run opens the desktop app and blocks until it quits. A second launch
// focuses the running app's window and exits.
func Run(ctx context.Context, opts Options) error {
	logw := opts.Log
	if logw == nil {
		logw = os.Stderr
	}
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(logw, "docket: "+format+"\n", args...)
	}

	// The handler is installed after application.New: a second instance
	// exits inside New, and must not have started reading pull requests.
	var assets lateHandler
	notes := newNotificationService(logf)
	var mainWin *application.WebviewWindow
	show := func(path string) {
		if mainWin == nil {
			return
		}
		if path != "" {
			mainWin.SetURL(path)
		}
		mainWin.Restore()
		mainWin.Show()
		mainWin.Focus()
	}
	notes.setOnOpen(show)

	wails := application.New(application.Options{
		Name:        AppName,
		Description: "Pull request review built around your attention.",
		Icon:        appIcon,
		Assets:      application.AssetOptions{Handler: &assets},
		Services:    []application.Service{application.NewService(notes)},
		// Run handles SIGINT/SIGTERM itself so it can quit through the app.
		DisableDefaultSignalHandler: true,
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               BundleID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { show("") },
		},
		Mac: application.MacOptions{
			// Docket keeps reading pull requests with its window closed.
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Linux: application.LinuxOptions{ApplicationID: BundleID, ProgramName: "docket"},
	})

	// An app opened from the Dock does not get the shell's PATH, so the
	// agent and gh would not be found without this.
	if _, err := loginenv.Prepare(ctx); err != nil {
		logf("could not load your shell environment (gh or the agent may not be found on PATH): %v", err)
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	a, err := app.Start(runCtx, app.Options{Demo: opts.Demo, Log: logw})
	if err != nil {
		return err
	}
	a.Server.OpenURL = func(u string) {
		if err := wails.Browser.OpenURL(u); err != nil {
			logf("could not open %s: %v", u, err)
		}
	}
	asked := false
	a.Engine.Notify = func(title, body, key string) {
		if err := notes.usable(); err != nil {
			return
		}
		if !asked {
			asked = true
			if ok, err := notes.authorize(); err != nil || !ok {
				logf("%s", notifyPermissionMessage)
				return
			}
		}
		if err := notes.sendAction("ready-"+key, title, body, notifyLabel, reviewPath(key)); err != nil {
			logf("notification: %v", err)
		}
	}
	assets.set(a.Server.Handler())

	mainWin = wails.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             mainWindowName,
		Title:            AppName,
		URL:              "/",
		Width:            mainWidth,
		Height:           mainHeight,
		MinWidth:         minWidth,
		MinHeight:        minHeight,
		BackgroundColour: film,
		InitialPosition:  application.WindowCentered,
		Linux:            application.LinuxWindow{Icon: appIcon},
	})

	ran := make(chan struct{})
	defer close(ran)
	wails.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			select {
			case <-runCtx.Done():
				wails.Quit()
			case <-ran:
			}
		}()
	})
	return wails.Run()
}

// reviewPath is the in-app address of a review: /#/r/<owner>/<repo>/<n>/brief.
func reviewPath(key string) string {
	repo, n, ok := strings.Cut(key, "#")
	owner, name, ok2 := strings.Cut(repo, "/")
	if !ok || !ok2 {
		return "/"
	}
	return fmt.Sprintf("/#/r/%s/%s/%s/brief", url.PathEscape(owner), url.PathEscape(name), n)
}

// lateHandler lets application.New take the asset handler before the server
// exists.
type lateHandler struct{ h atomic.Pointer[http.Handler] }

func (l *lateHandler) set(h http.Handler) { l.h.Store(&h) }

func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := l.h.Load()
	if h == nil {
		http.Error(w, "Docket is starting", http.StatusServiceUnavailable)
		return
	}
	(*h).ServeHTTP(w, r)
}
