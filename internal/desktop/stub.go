//go:build !desktop

package desktop

import (
	"context"
	"errors"
)

// Available reports whether this binary was built with the desktop app. It
// was not: build with -tags desktop.
func Available() bool { return false }

// Run is unavailable in this build.
func Run(context.Context, Options) error {
	return errors.New("this build has no desktop window: build with -tags desktop")
}
