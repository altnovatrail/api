package learning

import (
	"context"

	learnermodel "github.com/altnovatrail/learner-model"
	"github.com/altnovatrail/module-kit/modulepackage"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/altnovatrail/tutor"

	"github.com/altnovatrail/api/internal/admission"
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

func (f readerFunc) ReadSheet(ctx context.Context, r tutor.SheetReading, _ formatv0.Task, _ string) tutor.SheetRead {
	return f(ctx, r)
}

// WithAdmits is whether a kind of call that reads the work is admitted, in place
// of the gate, for every learner and route: a test's own, since no entry of
// tutor v0.1.35 admits the reading of a workbook page (В72).
func WithAdmits(admits func(call tutor.Call) bool) Option {
	return func(s *Service) { s.admits = admitsFunc(admits) }
}

// admitsFunc is a function as an admitter.
type admitsFunc func(call tutor.Call) bool

func (f admitsFunc) Reads(call tutor.Call, _ formatv0.AgeCategory, _ string) bool { return f(call) }

// WithWorkbookReader is what reads the work of a workbook page, in place of
// tutor: a test's own readers, for want of a provider and of an entry that
// admits the reading.
func WithWorkbookReader(read func(ctx context.Context, r tutor.WorkbookReading) tutor.WorkbookRead) Option {
	return func(s *Service) { s.workbook = workbookFunc(read) }
}

// workbookFunc is a function as a workbookReader.
type workbookFunc func(ctx context.Context, r tutor.WorkbookReading) tutor.WorkbookRead

func (f workbookFunc) ReadWorkbook(ctx context.Context, r tutor.WorkbookReading, _ formatv0.Task, _ string) tutor.WorkbookRead {
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

func (f tellerFunc) TellWords(ctx context.Context, w tutor.WordsRequest, _ string) tutor.WordsAnswer {
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

// WithCorrect is what words the tutor's words after a correct answer, in place
// of tutor: a test's own, admitted for every learner and route, so that no test
// calls a provider. It is no stub: the day of the privacy policy holds for it,
// as for a provider.
func WithCorrect(tell func(ctx context.Context, c tutor.CorrectRequest) tutor.CorrectAnswer) Option {
	return func(s *Service) { s.praiser = praiserFunc(tell) }
}

// praiserFunc is a function as a correctTeller that admits every request.
type praiserFunc func(ctx context.Context, c tutor.CorrectRequest) tutor.CorrectAnswer

func (f praiserFunc) CorrectWords(formatv0.AgeCategory, string) bool { return true }

func (f praiserFunc) TellCorrectWords(ctx context.Context, c tutor.CorrectRequest, _ string) tutor.CorrectAnswer {
	return f(ctx, c)
}

// GateTutor is the gate's adapter of the ports of tutor, which a Service runs
// on with no stub: it drops the own key's value that the stub alone reads.
func GateTutor(g *admission.Gate) interface {
	sheetReader
	wordsTeller
	correctTeller
	admitter
	workbookReader
} {
	return gateTutor{g}
}
