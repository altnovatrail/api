package logging_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"
	"testing"

	"github.com/altnovatrail/api/internal/logging"
)

func TestAnErrorIsNamedByItsChainAndNotItsText(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, "nil"},
		{"plain", errors.New(email), "*errors.errorString"},
		{
			"a path and an errno",
			fmt.Errorf("profile %s: %w", uid, &fs.PathError{Op: "open", Path: "/home/" + uid, Err: syscall.ENOENT}),
			"*fmt.wrapError > *fs.PathError > errno: no such file or directory",
		},
		{"a sentinel", fmt.Errorf("request of %s: %w", email, context.Canceled), "*fmt.wrapError > context.Canceled"},
		{"a deadline", context.DeadlineExceeded, "context.DeadlineExceeded"},
		{"joined", errors.Join(errSynthetic{}, errors.New(uid)), "*errors.joinError[2]"},
		{"uncomparable", uncomparable{nil}, "logging_test.uncomparable"},
		{"uncomparable by its value", fmt.Errorf("x: %w", holding{inner: []int{1}}), "*fmt.wrapError > logging_test.holding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := logging.ErrorKind(tc.err).String()
			if got != tc.want {
				t.Errorf("ErrorKind = %q, want %q", got, tc.want)
			}
			for _, value := range []string{email, uid} {
				if strings.Contains(got, value) {
					t.Errorf("ErrorKind = %q holds %q", got, value)
				}
			}
		})
	}
}

func TestALongChainIsCut(t *testing.T) {
	t.Parallel()

	err := error(errSynthetic{})
	for range 20 {
		err = fmt.Errorf("again: %w", err)
	}
	if got := strings.Count(logging.ErrorKind(err).String(), " > "); got != 7 {
		t.Errorf("a chain of 21 links is named by %d, want 8", got+1)
	}
}

func TestAPanicIsNamedByWhatItIs(t *testing.T) {
	t.Parallel()

	if got, want := logging.PanicKind(work).String(), "a value of type string"; got != want {
		t.Errorf("PanicKind(a string) = %q, want %q", got, want)
	}
	if got, want := logging.PanicKind(fakeFault{}).String(), "a value of type logging_test.fakeFault"; got != want {
		t.Errorf("PanicKind(a fault that is not the runtime's) = %q, want %q", got, want)
	}

	var fault any
	func() {
		defer func() { fault = recover() }()
		var values []int
		_ = values[len(work)]
	}()
	if got, want := logging.PanicKind(fault).String(), "runtime error: index out of range [16] with length 0"; got != want {
		t.Errorf("PanicKind(an index out of range) = %q, want %q", got, want)
	}
	wrapped := fmt.Errorf("handler of %s: %w", email, fault.(error))
	if got := logging.PanicKind(wrapped).String(); !strings.HasPrefix(got, "runtime error:") {
		t.Errorf("PanicKind(a wrapped fault of the runtime) = %q, want the runtime's words", got)
	}
}

// uncomparable is an error a map of sentinels cannot be looked up by.
type uncomparable struct{ links []error }

func (uncomparable) Error() string { return uid }

// holding is of a comparable type, and not comparable when inner holds a slice.
type holding struct{ inner any }

func (holding) Error() string { return uid }

// fakeFault calls itself a runtime error, and is not one.
type fakeFault struct{}

func (fakeFault) Error() string { return work }
func (fakeFault) RuntimeError() {}
