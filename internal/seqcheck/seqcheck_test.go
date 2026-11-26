package seqcheck

import "testing"

func TestObserve(t *testing.T) {
	const reset = int64(-1) // sentinel in a sequence: call Reset instead of Observe
	tests := []struct {
		name string
		seqs []int64
		want []Result // one per non-reset entry
	}{
		{
			name: "contiguous from zero",
			seqs: []int64{0, 1, 2, 3},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: OK}, {Kind: OK}},
		},
		{
			name: "first message sets baseline at any value",
			seqs: []int64{1000, 1001},
			want: []Result{{Kind: OK}, {Kind: OK}},
		},
		{
			name: "single missing",
			seqs: []int64{0, 1, 3, 4},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: Gap, Expected: 2, Got: 3, Missing: 1}, {Kind: OK}},
		},
		{
			name: "large gap then continues from new number",
			seqs: []int64{5, 6, 106, 107},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: Gap, Expected: 7, Got: 106, Missing: 99}, {Kind: OK}},
		},
		{
			name: "two gaps",
			seqs: []int64{0, 2, 5},
			want: []Result{{Kind: OK}, {Kind: Gap, Expected: 1, Got: 2, Missing: 1}, {Kind: Gap, Expected: 3, Got: 5, Missing: 2}},
		},
		{
			name: "duplicate is behind and does not move expectation",
			seqs: []int64{0, 1, 1, 2},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: Behind, Expected: 2, Got: 1}, {Kind: OK}},
		},
		{
			name: "out of order",
			seqs: []int64{10, 11, 9, 12},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: Behind, Expected: 12, Got: 9}, {Kind: OK}},
		},
		{
			name: "reset on reconnect: restart from zero is not a gap or behind",
			seqs: []int64{0, 1, 2, reset, 0, 1},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: OK}, {Kind: OK}, {Kind: OK}},
		},
		{
			name: "reset on reconnect: new baseline far ahead is not a gap",
			seqs: []int64{0, 1, reset, 500, 502},
			want: []Result{{Kind: OK}, {Kind: OK}, {Kind: OK}, {Kind: Gap, Expected: 501, Got: 502, Missing: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New()
			var got []Result
			for _, s := range tt.seqs {
				if s == reset {
					c.Reset()
					continue
				}
				got = append(got, c.Observe(s))
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d results, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("result %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestKindString(t *testing.T) {
	for k, want := range map[Kind]string{OK: "ok", Gap: "gap", Behind: "behind", Kind(42): "unknown"} {
		if k.String() != want {
			t.Errorf("Kind(%d).String() = %q, want %q", k, k.String(), want)
		}
	}
}
