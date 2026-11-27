package config

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

// maxLog is the size at which docket.log moves to docket.log.1.
const maxLog = 5 << 20

// OpenLog returns a writer for Docket's own log lines: stderr, and
// docket.log in Docket's home with each line stamped with its time, so what
// happened stays readable when the app was started from the Dock and has no
// terminal. The previous log is kept as docket.log.1 once it grows past
// 5 MiB. When the file cannot be opened the writer is stderr alone.
func OpenLog() (w io.Writer, closeLog func()) {
	home, err := Home()
	if err != nil {
		return os.Stderr, func() {}
	}
	p := filepath.Join(home, "docket.log")
	if st, err := os.Stat(p); err == nil && st.Size() > maxLog {
		_ = os.Rename(p, p+".1")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return os.Stderr, func() {}
	}
	return io.MultiWriter(os.Stderr, stamped{f}), func() { _ = f.Close() }
}

// stamped puts the time before each write; Docket writes one line at a time.
type stamped struct{ w io.Writer }

func (s stamped) Write(p []byte) (int, error) {
	if _, err := io.WriteString(s.w, time.Now().Format("2006-01-02 15:04:05 ")); err != nil {
		return 0, err
	}
	return s.w.Write(p)
}
