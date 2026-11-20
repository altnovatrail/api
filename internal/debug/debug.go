// Package debug is the debug mode of a local run with models (R97; R163 spec):
// which models serve the calls of AI of a task from paper, past the admission
// list, and what each call returned and cost. Its choice and its calls are each
// account's, in the memory of the process alone: nothing of them goes to the log
// or to the database (R09 spec), and api makes a Store only with
// API_DEBUG_MODELS, which a deployment refuses.
package debug

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	paperv1 "github.com/altnovatrail/spec/contracts/novatrail/paper/v1"
	"github.com/altnovatrail/spec/types/admissionv0"
	"github.com/altnovatrail/tutor"
	"github.com/altnovatrail/tutor/admission"
	"github.com/altnovatrail/tutor/provider"
	"github.com/altnovatrail/tutor/provider/claude/catalog"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/altnovatrail/api/internal/quota"
)

// kept is how many actions an account keeps until they are taken: a tab that
// never takes them holds no more than these, the oldest dropped first.
const kept = 100

// anthropic is the one provider api gives tutor an adapter of (R54).
const anthropic admissionv0.Provider = "anthropic"

// Store is the choice and the actions of each account.
type Store struct {
	mu       sync.Mutex
	accounts map[string]*account
}

type account struct {
	first, second *admission.Model
	actions       []*v1.DebugAction
}

// New is a Store with no choice: each call has its model of the list.
func New() *Store { return &Store{accounts: map[string]*account{}} }

func (s *Store) of(accountID string) *account {
	a, ok := s.accounts[accountID]
	if !ok {
		a = &account{}
		s.accounts[accountID] = a
	}
	return a
}

// Models are the models that can be chosen: tutor's catalog of Claude, cheapest
// first.
func Models() []*v1.DebugModel {
	var out []*v1.DebugModel
	for _, m := range catalog.Models {
		out = append(out, &v1.DebugModel{Id: m.ID, Name: strings.TrimPrefix(m.Name, "Claude "), Provider: string(anthropic),
			InputMicroUsd: perMillion(m.Input), OutputMicroUsd: perMillion(m.Output),
			Efforts: efforts(catalog.Efforts), ThinkingOffEfforts: efforts(m.NoThinkingEfforts())})
	}
	return out
}

func perMillion(usd float64) int64 { return int64(math.Round(usd * 1e6)) }

// The efforts of the contract by tutor's names.
var effortOf = map[string]v1.DebugEffort{
	"low": v1.DebugEffort_DEBUG_EFFORT_LOW, "medium": v1.DebugEffort_DEBUG_EFFORT_MEDIUM,
	"high": v1.DebugEffort_DEBUG_EFFORT_HIGH, "xhigh": v1.DebugEffort_DEBUG_EFFORT_XHIGH,
	"max": v1.DebugEffort_DEBUG_EFFORT_MAX,
}

func efforts(names []string) []v1.DebugEffort {
	var out []v1.DebugEffort
	for _, n := range names {
		out = append(out, effortOf[n])
	}
	return out
}

func effortName(e v1.DebugEffort) (string, bool) {
	if e == v1.DebugEffort_DEBUG_EFFORT_UNSPECIFIED {
		return "medium", true
	}
	for name, v := range effortOf {
		if v == e {
			return name, true
		}
	}
	return "", false
}

// Choice is the account's choice now.
func (s *Store) Choice(accountID string) *v1.DebugChoice {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.of(accountID)
	return &v1.DebugChoice{First: choiceOf(a.first), Second: choiceOf(a.second)}
}

// Chosen is whether the account has a first model, which bypasses the
// admission list as tutor does (R97); a nil Store has none.
func (s *Store) Chosen(accountID string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.of(accountID).first != nil
}

func choiceOf(m *admission.Model) *v1.DebugModelChoice {
	if m == nil {
		return nil
	}
	return &v1.DebugModelChoice{Model: m.ID, Effort: effortOf[m.Effort], Thinking: !m.NoThinking}
}

// ErrChoice is a choice of a model the catalog does not name, an effort it does
// not take, or thinking off where the model or the effort keeps it on.
var ErrChoice = errors.New("debug: not a choice of the models")

// SetChoice sets the account's choice, from its next action on.
func (s *Store) SetChoice(accountID string, c *v1.DebugChoice) (*v1.DebugChoice, error) {
	first, err := modelOf(c.GetFirst())
	if err != nil {
		return nil, fmt.Errorf("first: %w", err)
	}
	second, err := modelOf(c.GetSecond())
	if err != nil {
		return nil, fmt.Errorf("second: %w", err)
	}
	s.mu.Lock()
	a := s.of(accountID)
	a.first, a.second = first, second
	s.mu.Unlock()
	return s.Choice(accountID), nil
}

func modelOf(c *v1.DebugModelChoice) (*admission.Model, error) {
	if c == nil {
		return nil, nil
	}
	i := slices.IndexFunc(catalog.Models, func(m catalog.Model) bool { return m.ID == c.GetModel() })
	if i < 0 {
		return nil, fmt.Errorf("%w: model %q", ErrChoice, c.GetModel())
	}
	effort, ok := effortName(c.GetEffort())
	if !ok {
		return nil, fmt.Errorf("%w: effort %d", ErrChoice, c.GetEffort())
	}
	if !c.GetThinking() && !slices.Contains(catalog.Models[i].NoThinkingEfforts(), effort) {
		return nil, fmt.Errorf("%w: %s thinks at %s", ErrChoice, c.GetModel(), effort)
	}
	return &admission.Model{Provider: anthropic, ID: c.GetModel(), Effort: effort, NoThinking: !c.GetThinking()}, nil
}

// Take gives the account's actions since the last take, oldest first, and
// forgets them.
func (s *Store) Take(accountID string) []*v1.DebugAction {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.of(accountID)
	taken := a.actions
	a.actions = nil
	return taken
}

// Action is one request of a task from paper while it runs: the route it gives
// tutor, and the calls it records. A nil Action, of a run without the mode,
// records nothing and leaves a route as it is.
type Action struct {
	store   *Store
	account string
	first   *admission.Model
	second  *admission.Model
	ownKey  bool
	mu      sync.Mutex
	action  *v1.DebugAction
}

// Begin is an action of the account by its choice now; a nil Store begins none.
func (s *Store) Begin(accountID string, kind v1.DebugActionKind, attemptID string, at time.Time) *Action {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.of(accountID)
	return &Action{store: s, account: accountID, first: a.first, second: a.second,
		action: &v1.DebugAction{Kind: kind, AttemptId: attemptID, At: timestamppb.New(at)}}
}

// Route is r with the mode of the action: its models and its record.
func (a *Action) Route(r tutor.Route) tutor.Route {
	if a == nil {
		return r
	}
	a.ownKey = r.AI == admissionv0.AiRouteOwnKey
	r.Debug = &tutor.Debug{First: a.first, Second: a.second, Record: a.record}
	return r
}

// Pair says whether the action reads the work by two readers.
func (a *Action) Pair() bool { return a != nil && a.second != nil }

// Chosen is whether the action has a first model, by which tutor takes a call
// the admission list refuses.
func (a *Action) Chosen() bool { return a != nil && a.first != nil }

// record is tutor's record of a call of a model; the calls of one action may
// come from two goroutines at once.
func (a *Action) record(c tutor.DebugCall) {
	call := &v1.DebugCall{Kind: callKinds[c.Call], Reader: int32(c.Reader), Attempt: int32(c.Attempt), Model: c.Model.ID,
		Effort: effortOf[c.Model.Effort], Thinking: !c.Model.NoThinking, Answer: c.Answer}
	if u := c.Usage; u != nil {
		var written int64
		for _, w := range u.CacheWrites {
			written += w.Tokens
		}
		// A call that broke off before the provider named its model keeps the one asked.
		if u.Model != "" {
			call.Model = u.Model
		}
		call.InputTokens, call.CacheWriteTokens, call.CacheReadTokens = u.InputTokens, written, u.CacheReadTokens
		call.OutputTokens, call.ThinkingTokens = u.OutputTokens, u.ThinkingTokens
		call.DurationMs, call.FirstTokenMs, call.Retries = u.Latency.Milliseconds(), u.FirstToken.Milliseconds(), int32(u.Retries)
		call.CostMicroUsd, call.Billed = int64(quota.USD(u.USD)), u.Billed
	}
	if c.Err != nil {
		call.Failure = a.cause(c.Err)
	}
	a.add(call)
}

// cause is what a failed call came to, as the outcome of a request names it: a
// limit is the platform's on platform AI and the key's own on an own key.
func (a *Action) cause(err error) v1.Cause {
	var le *provider.LimitError
	switch {
	case errors.As(err, &le) && a.ownKey:
		return v1.Cause_CAUSE_LIMIT_REACHED
	case errors.As(err, &le):
		return v1.Cause_CAUSE_PLATFORM_LIMIT
	case errors.Is(err, provider.ErrKeyRefused):
		return v1.Cause_CAUSE_KEY_REFUSED
	}
	return v1.Cause_CAUSE_AI_FAILURE
}

var callKinds = map[tutor.Call]v1.DebugCallKind{
	tutor.CallFinding:    v1.DebugCallKind_DEBUG_CALL_KIND_TASK_FINDING,
	tutor.CallStatement:  v1.DebugCallKind_DEBUG_CALL_KIND_STATEMENT_READING,
	tutor.CallSolving:    v1.DebugCallKind_DEBUG_CALL_KIND_TASK_SOLVING,
	tutor.CallPaper:      v1.DebugCallKind_DEBUG_CALL_KIND_WORK_READING,
	tutor.CallPaperWords: v1.DebugCallKind_DEBUG_CALL_KIND_TUTOR_WORDS,
	tutor.CallPaperHint:  v1.DebugCallKind_DEBUG_CALL_KIND_TUTOR_WORDS,
}

// Solved records the call of module-kit's solver on the formulas the
// statement's tasks read into: the key of the crop as it gave it, each task
// with its statement and the solver's answer, none where it gave none.
func (a *Action) Solved(key *paperv1.Key, took time.Duration) {
	if a == nil {
		return
	}
	call := &v1.DebugCall{Kind: v1.DebugCallKind_DEBUG_CALL_KIND_SOLVER, Attempt: 1, DurationMs: took.Milliseconds()}
	if key != nil {
		// A key of the contract always marshals.
		written, _ := protojson.Marshal(key)
		call.Answer = string(written)
	}
	a.add(call)
}

func (a *Action) add(call *v1.DebugCall) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.action.Calls = append(a.action.Calls, call)
}

// Differ records whether the two readers read a place of the work differently.
func (a *Action) Differ(differ bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.action.ReadersDiffer = differ
}

// End keeps the action for the account's next take, when it made a call: a
// repeat or a refusal before any call keeps none.
func (a *Action) End() {
	if a == nil {
		return
	}
	a.mu.Lock()
	done := proto.Clone(a.action).(*v1.DebugAction)
	a.mu.Unlock()
	if len(done.Calls) == 0 {
		return
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	acc := a.store.of(a.account)
	acc.actions = append(acc.actions, done)
	if over := len(acc.actions) - kept; over > 0 {
		acc.actions = slices.Delete(acc.actions, 0, over)
	}
}
