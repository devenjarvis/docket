// Package app wires Docket together: the same engine and server whether the
// front is the desktop window or a browser tab.
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/gh"
	"github.com/devenjarvis/docket/internal/repo"
	"github.com/devenjarvis/docket/internal/review"
	"github.com/devenjarvis/docket/internal/rules"
	"github.com/devenjarvis/docket/internal/server"
)

type Options struct {
	// Demo uses the sample pull requests and the scripted agent, in a home
	// directory of its own.
	Demo bool
	Log  io.Writer
}

// App is a running Docket.
type App struct {
	Server *server.Server
	Engine *review.Engine
	Home   string
}

// Start builds the engine and server and begins watching for pull requests.
// It runs until ctx ends.
func Start(ctx context.Context, opts Options) (*App, error) {
	logw := opts.Log
	if logw == nil {
		logw = os.Stderr
	}
	logf := func(format string, args ...any) { fmt.Fprintf(logw, "docket: "+format+"\n", args...) }

	home, err := config.Home()
	if err != nil {
		return nil, err
	}
	var ghc gh.Client = gh.CLI{}
	var prep repo.Preparer = repo.Git{Home: home}
	if opts.Demo {
		if os.Getenv("DOCKET_HOME") == "" {
			home = filepath.Join(os.TempDir(), "docket-demo")
			_ = os.RemoveAll(home)
			if err := os.MkdirAll(home, 0o755); err != nil {
				return nil, err
			}
			// config reads DOCKET_HOME, so settings changed in a demo run stay
			// in the demo's directory.
			_ = os.Setenv("DOCKET_HOME", home)
		}
		ghc, prep = demo.GitHub{Home: home}, demo.Checkout{Home: home}
	}
	rs, err := rules.Open(home)
	if err != nil {
		return nil, err
	}
	if opts.Demo {
		demo.Seed(home, rs)
	}
	eng := review.New(home, ghc, prep, rs)
	if in, ok := ghc.(gh.Inbox); ok {
		eng.Inbox = in
	}
	if br, ok := prep.(repo.Brancher); ok {
		eng.Branches = br
	}
	eng.TendMode = func() string {
		c, _ := config.Read()
		return c.TendMode()
	}
	eng.Logf = logf
	srv := server.New(eng, rs, home)
	if opts.Demo {
		self, err := os.Executable()
		if err != nil {
			return nil, err
		}
		srv.Demo, srv.DemoAgent = true, fmt.Sprintf("%q fake-agent", self)
	}
	eng.Agent = srv.AgentCommand
	eng.RunCode = func() bool {
		c, _ := config.Read()
		return c.Runs()
	}
	c, _ := config.Read()
	eng.Start(ctx, c.Workers(), time.Duration(c.Poll())*time.Second)
	return &App{Server: srv, Engine: eng, Home: home}, nil
}
