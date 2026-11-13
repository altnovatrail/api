package learning

import (
	"context"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
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

// WithWords is what words the tutor's words, in place of tutor: a test's own,
// admitted for every learner and route, so that no test calls a provider. The
// real gate admits the words at the author's stage since tutor v0.1.24 (R54), so
// a test on it must not ask for them.
func WithWords(tell func(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer) Option {
	return func(s *Service) { s.teller = tellerFunc(tell) }
}

// tellerFunc is a function as a wordsTeller that admits every request.
type tellerFunc func(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer

func (f tellerFunc) Words(bool, formatv0.AgeCategory, string) bool { return true }

func (f tellerFunc) TellWords(ctx context.Context, w tutor.WordsRequest) tutor.WordsAnswer {
	return f(ctx, w)
}

// Candidates is the full IDs of the tasks of pkg the engine's pool holds for a
// learner of category on the route of ownKey — the own key of that provider, or
// platform AI —, and how many of its lessons it gets: those this code begins.
func (s *Service) Candidates(pkg *modulepackage.Package, category formatv0.AgeCategory, ownKey string) (pool []string, lessons int, err error) {
	m, _, err := s.candidates(pkg, learnermodel.Graph{}, category, ownKey, true)
	for _, c := range m.Pool {
		pool = append(pool, c.Task.ID)
	}
	return pool, len(m.Lessons), err
}

// WithPrice is what a call is expected to cost, in place of tutor's prices: a
// test's own, so that its reserves are known.
func WithPrice(price func(call tutor.Call, route tutor.Route) tutor.CallPrice) Option {
	return func(s *Service) { s.price = price }
}
