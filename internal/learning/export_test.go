package learning

import (
	"context"

	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"
)

// WithModuleKit is the version of module-kit a Service says it is built with,
// for a test of a build that recorded none.
func WithModuleKit(version string) Option { return func(s *Service) { s.moduleKit = version } }

// WithReader is what reads the work of a handwriting sheet, in place of tutor:
// a test's own readers, for want of a provider.
func WithReader(read func(ctx context.Context, r tutor.SheetReading) tutor.SheetRead) Option {
	return func(s *Service) { s.reader = readerFunc(read) }
}

// readerFunc is a function as a sheetReader.
type readerFunc func(ctx context.Context, r tutor.SheetReading) tutor.SheetRead

func (f readerFunc) ReadSheet(ctx context.Context, r tutor.SheetReading) tutor.SheetRead {
	return f(ctx, r)
}

// WithWords is what words the tutor's words, in place of tutor, which admits
// no model for them yet: a test's own, admitted for every learner and route,
// for want of a provider.
func WithWords(tell func(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer) Option {
	return func(s *Service) { s.teller = tellerFunc(tell) }
}

// tellerFunc is a function as a wordsTeller that admits every request.
type tellerFunc func(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer

func (f tellerFunc) Words(bool, formatv0.AgeCategory, string) bool { return true }

func (f tellerFunc) TellWords(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer {
	return f(ctx, w)
}
