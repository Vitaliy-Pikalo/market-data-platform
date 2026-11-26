package sink

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

// write is one Write call at a given instant.
type write struct {
	at   string
	line string
}

func TestRotation(t *testing.T) {
	tests := []struct {
		name   string
		writes []write
		want   map[string]string // relative path -> decompressed content
	}{
		{
			name:   "single hour",
			writes: []write{{"2026-11-26T10:00:00Z", "a"}, {"2026-11-26T10:59:59.999Z", "b"}},
			want:   map[string]string{"2026-11-26/10.jsonl.zst": "a\nb\n"},
		},
		{
			name:   "rotates on hour boundary",
			writes: []write{{"2026-11-26T10:59:59.999999999Z", "a"}, {"2026-11-26T11:00:00Z", "b"}},
			want: map[string]string{
				"2026-11-26/10.jsonl.zst": "a\n",
				"2026-11-26/11.jsonl.zst": "b\n",
			},
		},
		{
			name:   "rotates across midnight into new day dir",
			writes: []write{{"2026-11-26T23:30:00Z", "a"}, {"2026-11-27T00:00:01Z", "b"}},
			want: map[string]string{
				"2026-11-26/23.jsonl.zst": "a\n",
				"2026-11-27/00.jsonl.zst": "b\n",
			},
		},
		{
			name:   "non-UTC clock is bucketed in UTC",
			writes: []write{{"2026-11-26T12:15:00+02:00", "a"}},
			want:   map[string]string{"2026-11-26/10.jsonl.zst": "a\n"},
		},
		{
			name: "skipped hours create no files",
			writes: []write{
				{"2026-11-26T08:10:00Z", "a"},
				{"2026-11-26T11:10:00Z", "b"},
				{"2026-11-26T11:20:00Z", "c"},
			},
			want: map[string]string{
				"2026-11-26/08.jsonl.zst": "a\n",
				"2026-11-26/11.jsonl.zst": "b\nc\n",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			clk := &fakeClock{}
			w := New(dir, WithClock(clk.now))
			for _, wr := range tt.writes {
				clk.t = ts(wr.at)
				if err := w.Write([]byte(wr.line)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			got := readTree(t, dir)
			if len(got) != len(tt.want) {
				t.Fatalf("files = %v, want %v", keys(got), keys(tt.want))
			}
			for p, content := range tt.want {
				if got[p] != content {
					t.Errorf("%s = %q, want %q", p, got[p], content)
				}
			}
		})
	}
}

func TestPartialUntilClose(t *testing.T) {
	dir := t.TempDir()
	clk := &fakeClock{t: ts("2026-11-26T10:00:00Z")}
	w := New(dir, WithClock(clk.now))
	if err := w.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "2026-11-26", "10.jsonl.zst")
	if !exists(final + ".partial") {
		t.Fatalf("expected %s.partial while open", final)
	}
	if exists(final) {
		t.Fatalf("final file %s must not exist while open", final)
	}
	if got := w.CurrentPath(); got != final {
		t.Errorf("CurrentPath = %q, want %q", got, final)
	}

	// Rotation closes the previous hour cleanly.
	clk.t = ts("2026-11-26T11:00:00Z")
	if err := w.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	if !exists(final) || exists(final+".partial") {
		t.Fatalf("after rotation want %s renamed from .partial", final)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	for p := range readTree(t, dir) {
		if strings.HasSuffix(p, ".partial") {
			t.Errorf("leftover partial after Close: %s", p)
		}
	}
}

func TestExistingFilesAreNotOverwritten(t *testing.T) {
	tests := []struct {
		name     string
		existing []string // files present before the writer starts
		wantNew  string
	}{
		{"no collision", nil, "2026-11-26/10.jsonl.zst"},
		{"clean file from earlier run", []string{"2026-11-26/10.jsonl.zst"}, "2026-11-26/10.1.jsonl.zst"},
		{"crashed partial", []string{"2026-11-26/10.jsonl.zst.partial"}, "2026-11-26/10.1.jsonl.zst"},
		{"several", []string{"2026-11-26/10.jsonl.zst", "2026-11-26/10.1.jsonl.zst.partial"}, "2026-11-26/10.2.jsonl.zst"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, p := range tt.existing {
				full := filepath.Join(dir, filepath.FromSlash(p))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			clk := &fakeClock{t: ts("2026-11-26T10:30:00Z")}
			w := New(dir, WithClock(clk.now))
			if err := w.Write([]byte("new")); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			for _, p := range tt.existing {
				b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
				if err != nil || string(b) != "old" {
					t.Errorf("existing %s modified: %q, %v", p, b, err)
				}
			}
			if got := readZstd(t, filepath.Join(dir, filepath.FromSlash(tt.wantNew))); got != "new\n" {
				t.Errorf("%s = %q, want %q", tt.wantNew, got, "new\n")
			}
		})
	}
}

// readTree returns relative slash paths -> content for every file under dir.
// .jsonl.zst files are decompressed.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if strings.HasSuffix(p, ".jsonl.zst") {
			out[rel] = readZstd(t, p)
		} else {
			out[rel] = ""
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readZstd(t *testing.T, p string) string {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	b, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("decode %s: %v", p, err)
	}
	return string(b)
}

func keys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
