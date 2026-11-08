package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"syscall"
)

// Kind names what an error or a panic is, and none of what it carries: its
// text may hold a path, an address, a value from a request. Only ErrorKind and
// PanicKind make one, and the table accepts no other value for the fields
// error_kind and panic_kind.
type Kind struct{ name string }

// String is the kind's name.
func (k Kind) String() string { return k.name }

// LogValue is what the log writes: the name.
func (k Kind) LogValue() slog.Value { return slog.StringValue(k.name) }

// maxChain is how many links of an error's chain ErrorKind names.
const maxChain = 8

// sentinels are errors of the standard library whose text is fixed by the
// code, and which say more by their name than by their type.
var sentinels = map[error]string{
	context.Canceled:         "context.Canceled",
	context.DeadlineExceeded: "context.DeadlineExceeded",
	net.ErrClosed:            "net.ErrClosed",
	http.ErrServerClosed:     "http.ErrServerClosed",
}

// ErrorKind names an error by its chain, outermost first: the type of each
// link, a sentinel of the standard library by its name, and an errno of the
// system in the system's own words, which hold no data. The text of any other
// link is not written, since an error made by fmt.Errorf holds whatever its
// author formatted into it.
func ErrorKind(err error) Kind {
	if err == nil {
		return Kind{name: "nil"}
	}
	var chain []string
	for link := err; link != nil && len(chain) < maxChain; {
		chain = append(chain, linkName(link))
		switch unwrapped := link.(type) {
		case interface{ Unwrap() error }:
			link = unwrapped.Unwrap()
		case interface{ Unwrap() []error }:
			chain[len(chain)-1] += fmt.Sprintf("[%d]", len(unwrapped.Unwrap()))
			link = nil
		default:
			link = nil
		}
	}
	return Kind{name: strings.Join(chain, " > ")}
}

// linkName is one link of a chain, by its name when it is a sentinel, in the
// system's words when it is an errno, and by its type otherwise.
func linkName(link error) string {
	// A sentinel is a comparable value; a link that is not would panic in the
	// map's lookup. The value is asked, not its type: a struct of a
	// comparable type is not comparable when a field of interface type holds
	// a slice.
	if reflect.ValueOf(link).Comparable() {
		if name, sentinel := sentinels[link]; sentinel {
			return name
		}
	}
	if errno, isErrno := link.(syscall.Errno); isErrno {
		return "errno: " + errno.Error()
	}
	return fmt.Sprintf("%T", link)
}

// PanicKind names what a panic was raised with. A fault of the Go runtime is
// put in the runtime's own words, found inside whatever wrapped it, because
// those words hold types and numbers and no data. Anything else is a value of
// ours or a library's, which may hold a task, an answer or an image, so only
// its type is written.
func PanicKind(recovered any) Kind {
	var fault runtime.Error
	if err, isError := recovered.(error); isError && errors.As(err, &fault) && ofTheRuntime(fault) {
		return Kind{name: fault.Error()}
	}
	return Kind{name: fmt.Sprintf("a value of type %T", recovered)}
}

// ofTheRuntime says whether a fault was made by the Go runtime itself. Any type
// can call itself a runtime error by having the method, and one that is not
// the runtime's may say anything at all.
func ofTheRuntime(fault runtime.Error) bool {
	kind := reflect.TypeOf(fault)
	for kind.Kind() == reflect.Pointer {
		kind = kind.Elem()
	}
	return kind.PkgPath() == "runtime"
}
