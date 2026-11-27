// Docket: pull request review built around the reviewer's attention.
//
//	docket             open the desktop app (or serve, in a build without it)
//	docket serve       serve the UI on a local port for a browser
//	docket demo        the same, with sample pull requests and a scripted agent
//	docket version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/devenjarvis/docket/internal/server"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/devenjarvis/docket/internal/app"
	"github.com/devenjarvis/docket/internal/config"
	"github.com/devenjarvis/docket/internal/demo"
	"github.com/devenjarvis/docket/internal/desktop"
	"github.com/devenjarvis/docket/internal/loginenv"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "docket:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := ""
	if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "version":
		fmt.Println("docket", version)
		return nil
	case "fake-agent":
		// The scripted agent the demo runs; it speaks ACP on stdio.
		return demo.ServeAgent(os.Stdin, os.Stdout)
	case "serve", "demo":
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		addr := fs.String("addr", "127.0.0.1:4127", "address to listen on")
		demoFlag := fs.Bool("demo", cmd == "demo", "use sample pull requests and a scripted agent")
		if err := fs.Parse(args); err != nil {
			return err
		}
		_, _ = loginenv.ApplyAgentPath()
		return serve(ctx, *addr, *demoFlag)
	case "":
		fs := flag.NewFlagSet("docket", flag.ContinueOnError)
		demoFlag := fs.Bool("demo", false, "use sample pull requests and a scripted agent")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if !desktop.Available() {
			fmt.Fprintln(os.Stderr, "docket: this build has no desktop window (build with -tags desktop); serving in the browser instead")
			return serve(ctx, "127.0.0.1:4127", *demoFlag)
		}
		logw, closeLog := appLog(*demoFlag)
		defer closeLog()
		return desktop.Run(ctx, desktop.Options{Demo: *demoFlag, Log: logw})
	default:
		return fmt.Errorf("unknown command %q: try docket, docket serve or docket demo", cmd)
	}
}

func serve(ctx context.Context, addr string, demoMode bool) error {
	logw, closeLog := appLog(demoMode)
	defer closeLog()
	a, err := app.Start(ctx, app.Options{Demo: demoMode, Log: logw})
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "docket: open http://%s  (data in %s)\n", ln.Addr(), a.Home)
	srv := &http.Server{Handler: server.LocalOnly(a.Server.Handler())}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// appLog is where Docket's log lines go: stderr and ~/.docket/docket.log, or
// stderr alone for the demo, whose home is thrown away.
func appLog(demoMode bool) (io.Writer, func()) {
	if demoMode {
		return os.Stderr, func() {}
	}
	return config.OpenLog()
}
