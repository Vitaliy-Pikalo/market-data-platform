package backoff

import (
	"testing"
	"time"
)

func TestCap(t *testing.T) {
	b := New(DefaultBase, DefaultMax, DefaultResetAfter)
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 500 * time.Millisecond},
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
		{6, 30 * time.Second}, // 32s capped
		{7, 30 * time.Second},
		{100, 30 * time.Second}, // no overflow
		{10_000, 30 * time.Second},
	}
	for _, tt := range tests {
		if got := b.Cap(tt.attempt); got != tt.want {
			t.Errorf("Cap(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestNextBounds(t *testing.T) {
	tests := []struct {
		name string
		rand func(n int64) int64
		want []time.Duration // successive Next(0) results
	}{
		{
			name: "max jitter approaches cap",
			rand: func(n int64) int64 { return n - 1 },
			want: []time.Duration{
				500*time.Millisecond - 1, time.Second - 1, 2*time.Second - 1, 4*time.Second - 1,
				8*time.Second - 1, 16*time.Second - 1, 30*time.Second - 1, 30*time.Second - 1,
			},
		},
		{
			name: "min jitter is zero",
			rand: func(n int64) int64 { return 0 },
			want: []time.Duration{0, 0, 0, 0},
		},
		{
			name: "half jitter",
			rand: func(n int64) int64 { return n / 2 },
			want: []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(DefaultBase, DefaultMax, DefaultResetAfter)
			b.Rand = tt.rand
			for i, want := range tt.want {
				if got := b.Next(0); got != want {
					t.Errorf("Next #%d = %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestNextRealRandStaysInRange(t *testing.T) {
	b := New(DefaultBase, DefaultMax, DefaultResetAfter)
	for i := 0; i < 10_000; i++ {
		attempt := b.Attempt()
		d := b.Next(0)
		if d < 0 || d >= b.Cap(attempt) {
			t.Fatalf("attempt %d: Next = %v, want [0, %v)", attempt, d, b.Cap(attempt))
		}
	}
}

func TestReset(t *testing.T) {
	maxRand := func(n int64) int64 { return n - 1 }
	tests := []struct {
		name        string
		healthy     []time.Duration // healthy uptime passed to successive Next calls
		wantAttempt int             // Attempt() after all calls
		wantLast    time.Duration
	}{
		{"failures escalate", []time.Duration{0, 0, 0}, 3, 2*time.Second - 1},
		{"short connection does not reset", []time.Duration{0, 0, 59 * time.Second}, 3, 2*time.Second - 1},
		{"exactly ResetAfter resets", []time.Duration{0, 0, 60 * time.Second}, 1, 500*time.Millisecond - 1},
		{"long connection resets", []time.Duration{0, 0, 0, 0, time.Hour}, 1, 500*time.Millisecond - 1},
		{"escalates again after reset", []time.Duration{0, 0, time.Hour, 0}, 2, time.Second - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(DefaultBase, DefaultMax, DefaultResetAfter)
			b.Rand = maxRand
			var last time.Duration
			for _, h := range tt.healthy {
				last = b.Next(h)
			}
			if b.Attempt() != tt.wantAttempt {
				t.Errorf("Attempt = %d, want %d", b.Attempt(), tt.wantAttempt)
			}
			if last != tt.wantLast {
				t.Errorf("last Next = %v, want %v", last, tt.wantLast)
			}
		})
	}
}
