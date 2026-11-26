// Package sink writes newline-delimited records to hourly zstd-compressed files.
//
// Files are laid out as <dir>/YYYY-MM-DD/HH.jsonl.zst (UTC). While a file is
// open it is named HH.jsonl.zst.partial; it is renamed to its final name only
// when it is closed cleanly (hour rotation or Close). A leftover .partial file
// therefore marks a file whose writer crashed.
//
// The zstd encoder is flushed from Write at most every flush interval
// (default 5s), so after a crash a .partial file can be decoded up to the
// last flush; the decoder then reports io.ErrUnexpectedEOF because the frame
// was never finished.
package sink

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/klauspost/compress/zstd"
)

const partialSuffix = ".partial"

// DefaultFlushInterval is how often Write flushes the zstd encoder by default.
const DefaultFlushInterval = 5 * time.Second

// Writer is an hourly-rotating zstd JSONL writer. It is not safe for
// concurrent use.
type Writer struct {
	dir        string
	now        func() time.Time
	flushEvery time.Duration

	hour      time.Time // start of the hour the open file belongs to (UTC)
	path      string    // final path of the open file
	file      *os.File
	enc       *zstd.Encoder
	bytes     int64     // uncompressed bytes written to the open file
	lastFlush time.Time // when the encoder was last flushed (or the file opened)
}

// Option configures a Writer.
type Option func(*Writer)

// WithClock overrides the clock used to pick the hourly file. Intended for tests.
func WithClock(now func() time.Time) Option {
	return func(w *Writer) { w.now = now }
}

// WithFlushInterval sets how often Write flushes the zstd encoder to the file.
// d <= 0 disables periodic flushing (data then reaches the file only when an
// encoder block fills up, or on rotation/Close).
func WithFlushInterval(d time.Duration) Option {
	return func(w *Writer) { w.flushEvery = d }
}

// New returns a Writer that writes under dir. No file is created until the
// first Write.
func New(dir string, opts ...Option) *Writer {
	w := &Writer{dir: dir, now: time.Now, flushEvery: DefaultFlushInterval}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Write appends line followed by '\n' to the file for the current UTC hour,
// rotating first if the hour changed, and flushes the encoder if the flush
// interval has passed. line must not contain '\n'.
func (w *Writer) Write(line []byte) error {
	now := w.now()
	hour := now.UTC().Truncate(time.Hour)
	if w.file == nil || !hour.Equal(w.hour) {
		if err := w.closeCurrent(); err != nil {
			return err
		}
		if err := w.open(hour, now); err != nil {
			return err
		}
	}
	if _, err := w.enc.Write(line); err != nil {
		return fmt.Errorf("sink: write %s: %w", w.path, err)
	}
	if _, err := w.enc.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("sink: write %s: %w", w.path, err)
	}
	w.bytes += int64(len(line)) + 1
	if w.flushEvery > 0 && now.Sub(w.lastFlush) >= w.flushEvery {
		if err := w.enc.Flush(); err != nil {
			return fmt.Errorf("sink: flush %s: %w", w.path, err)
		}
		w.lastFlush = now
	}
	return nil
}

// Close flushes and closes the open file, if any, and renames it to its final name.
func (w *Writer) Close() error {
	return w.closeCurrent()
}

// CurrentPath returns the final path of the open file, or "" if none is open.
func (w *Writer) CurrentPath() string {
	if w.file == nil {
		return ""
	}
	return w.path
}

func (w *Writer) open(hour, now time.Time) error {
	dayDir := filepath.Join(w.dir, hour.Format("2006-01-02"))
	if err := os.MkdirAll(dayDir, 0o755); err != nil {
		return fmt.Errorf("sink: mkdir: %w", err)
	}
	path, err := freePath(dayDir, hour.Format("15"))
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path+partialSuffix, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("sink: create: %w", err)
	}
	enc, err := zstd.NewWriter(f, zstd.WithEncoderConcurrency(1))
	if err != nil {
		f.Close()
		return fmt.Errorf("sink: zstd: %w", err)
	}
	w.hour, w.path, w.file, w.enc, w.bytes, w.lastFlush = hour, path, f, enc, 0, now
	return nil
}

// freePath returns <dayDir>/<hh>.jsonl.zst, or <hh>.N.jsonl.zst for the
// smallest N >= 1 if that name (or its .partial) is already taken, e.g. after
// a restart within the same hour or a crash. Existing files are never
// overwritten or appended to.
func freePath(dayDir, hh string) (string, error) {
	for n := 0; n < 1000; n++ {
		name := hh + ".jsonl.zst"
		if n > 0 {
			name = fmt.Sprintf("%s.%d.jsonl.zst", hh, n)
		}
		p := filepath.Join(dayDir, name)
		if !exists(p) && !exists(p+partialSuffix) {
			return p, nil
		}
	}
	return "", fmt.Errorf("sink: no free file name for hour %s in %s", hh, dayDir)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (w *Writer) closeCurrent() error {
	if w.file == nil {
		return nil
	}
	errEnc := w.enc.Close()
	errSync := w.file.Sync()
	errFile := w.file.Close()
	w.file, w.enc = nil, nil
	if err := errors.Join(errEnc, errSync, errFile); err != nil {
		return fmt.Errorf("sink: close %s: %w", w.path, err)
	}
	if err := os.Rename(w.path+partialSuffix, w.path); err != nil {
		return fmt.Errorf("sink: rename: %w", err)
	}
	return nil
}
