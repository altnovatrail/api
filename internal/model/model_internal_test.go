package model

import "testing"

// A version is later by its numbers, without its v and what follows a - or a +,
// and one either side cannot read is no later.
func TestAVersionIsLaterByItsNumbers(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.1.10", "v0.1.9", true}, {"v0.1.9", "v0.1.10", false}, {"0.5", "0.4", true}, {"v1.0.0-rc.1", "v0.9.9", true},
		{"v1.0.0", "v1.0.0+build", false}, {"(devel)", "v0.1.4", false}, {"v-1", "v0.1.4", false}, {"", "v0.1.4", false},
	} {
		if got := later(c.a, c.b); got != c.want {
			t.Errorf("later(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
