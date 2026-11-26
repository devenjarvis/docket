// Package desktop is the Wails 3 shell: one window onto the same server
// `docket serve` runs, plus the things only a desktop app can do (system
// notifications when a pull request is ready, opening links in the browser,
// one running instance). Everything else lives in the server, so the app and
// the browser version cannot drift apart.
package desktop

import "io"

// Names the operating system shows.
const (
	AppName  = "Docket"
	BundleID = "dev.devenjarvis.docket"
)

type Options struct {
	// Demo uses sample pull requests and a scripted agent.
	Demo bool
	// Log receives the app's own log lines; nil means stderr.
	Log io.Writer
}
