// Package account is api's AccountService of the contract novatrail.api.v1: the
// account and its invite code, and, task by task, its PIN, profiles and
// deletion (R04, R34, R35, R36). Its methods run after the checks of architecture 3.1,
// which give them the caller (rpc.CallerFrom); a method it has no handler for
// yet answers unimplemented.
package account

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"

	"github.com/altnovatrail/api/internal/authz"
	"github.com/altnovatrail/api/internal/consentage"
	"github.com/altnovatrail/api/internal/deletion"
	"github.com/altnovatrail/api/internal/export"
	"github.com/altnovatrail/api/internal/forms"
	"github.com/altnovatrail/api/internal/invite"
	"github.com/altnovatrail/api/internal/legal"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/store"
)

// Service is the AccountService on the database api.
type Service struct {
	apiv1connect.UnimplementedAccountServiceHandler
	db      rpc.DB
	rights  Rights
	deleter *deletion.Deleter
	texts   *legal.Texts
	ages    *consentage.Table
	forms   forms.Store
	export  *export.Exporter
	now     func() time.Time
}

// Rights is what writes the tuple of a profile it makes, and deletes the tuples
// of what it deletes: authz.Rights.
type Rights interface {
	WriteOwner(ctx context.Context, accountID, profileID string) error
	Can(ctx context.Context, accountID string, right authz.Right, profileID string) (bool, error)
	deletion.Rights
}

// Option is a choice of New's that a test makes.
type Option func(*Service)

// WithClock is the clock the handlers go by — the PIN's count, the family day
// of the quota —, the system's by default.
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

// WithTexts is the legal texts the requests of a consent name, those of the
// tag by default (legal.Spec): a self-hosted copy's folder.
func WithTexts(texts *legal.Texts) Option { return func(s *Service) { s.texts = texts } }

// WithForms is the store of the scans of the forms of a parent's consent; with
// none, SendConsentForm is refused unavailable.
func WithForms(store forms.Store) Option { return func(s *Service) { s.forms = store } }

// WithExport is the exporter of the profiles and the account; with none, the
// export is refused unimplemented.
func WithExport(x *export.Exporter) Option { return func(s *Service) { s.export = x } }

// New is the AccountService on db, with the tuples of the profiles in rights
// and the users at signin.
func New(db rpc.DB, rights Rights, signin deletion.Signin, options ...Option) *Service {
	s := &Service{db: db, rights: rights, deleter: deletion.New(db, rights, signin), now: time.Now,
		texts: legal.Spec(), ages: consentage.Spec()}
	for _, option := range options {
		option(s)
	}
	return s
}

// GetAccount gives the account of the caller: whether its PIN is set, its
// profiles and its consents for a child with no profile. Open attempts and lesson
// runs come with epic E3, which keeps them.
func (s *Service) GetAccount(ctx context.Context, _ *v1.GetAccountRequest) (*v1.GetAccountResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	view, err := store.AccountView(ctx, s.db, caller.AccountID)
	if err != nil {
		return nil, refused(ctx, err)
	}
	return &v1.GetAccountResponse{Account: s.account(view)}, nil
}

// CreateAccount creates the caller's account by an invite code, the owner's
// statement that they are 18 or older and the current versions of
// privacy-policy and terms-of-use, which it records (R130, R136 spec): a text in
// force left out, or a version not current, is CAUSE_TEXT_NOT_CURRENT. A caller with an account already gets
// CAUSE_ALREADY_EXISTS before the statement and the code are looked at, so that
// a repeat after a lost answer gets that rather than a code redeemed (contract,
// CreateAccount).
func (s *Service) CreateAccount(ctx context.Context, req *v1.CreateAccountRequest) (*v1.CreateAccountResponse, error) {
	caller, _ := rpc.CallerFrom(ctx)
	if caller.AccountID != "" {
		return nil, alreadyExists(ctx)
	}
	code, ok := invite.Read(req.GetInviteCode())
	if !req.GetAdultStated() || !ok {
		return nil, s.malformed(ctx, caller.UID, req.GetAdultStated())
	}
	if !s.texts.Names(req.GetAcceptedTexts(), legal.PrivacyPolicy, legal.TermsOfUse) {
		return nil, textNotCurrent(ctx)
	}
	var texts []store.TextRef
	for _, ref := range req.GetAcceptedTexts() {
		texts = append(texts, store.TextRef{ID: ref.GetId(), Version: ref.GetVersion()})
	}
	id, err := store.CreateAccount(ctx, s.db, caller.UID, code[:], texts)
	if err != nil {
		return nil, refused(ctx, err)
	}
	return &v1.CreateAccountResponse{Account: &v1.Account{AccountId: id}}, nil
}

// malformed refuses a request whose statement or code will not do. An account
// made since the checks, by a request beside this one, still comes first, as
// the store puts it before the code: a repeat is told the account is there.
func (s *Service) malformed(ctx context.Context, uid string, adultStated bool) error {
	switch found, err := store.AccountByUID(ctx, s.db, uid); {
	case err == nil && found.Deleting:
		return rpc.AccountGone(ctx)
	case err == nil:
		return alreadyExists(ctx)
	case !errors.Is(err, store.ErrNoAccount):
		return rpc.Failed(ctx, err)
	}
	if !adultStated {
		return connect.NewError(connect.CodeInvalidArgument, "adult_stated")
	}
	return codeNotAccepted(ctx)
}

// refused answers a refusal of the store by the contract's cause, and anything
// else as a failure.
func refused(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, store.ErrAccountExists):
		return alreadyExists(ctx)
	case errors.Is(err, store.ErrCodeNotAccepted):
		return codeNotAccepted(ctx)
	case errors.Is(err, store.ErrAccountDeleting), errors.Is(err, store.ErrNoAccount):
		// Marked or deleted after the checks let the request through.
		return rpc.AccountGone(ctx)
	}
	return rpc.Failed(ctx, err)
}

func alreadyExists(ctx context.Context) error {
	return rpc.Refuse(ctx, connect.CodeAlreadyExists, v1.Cause_CAUSE_ALREADY_EXISTS)
}

func codeNotAccepted(ctx context.Context) error {
	return rpc.Refuse(ctx, connect.CodeInvalidArgument, v1.Cause_CAUSE_CODE_NOT_ACCEPTED)
}

// account is the view in the contract's words.
func (s *Service) account(view store.View) *v1.Account {
	out := &v1.Account{AccountId: view.ID, PinSet: view.PinSet}
	for _, p := range view.Profiles {
		out.Profiles = append(out.Profiles, s.profile(p))
	}
	for _, c := range view.ChildConsents {
		out.ChildConsents = append(out.ChildConsents, childConsent(c))
	}
	return out
}

func (s *Service) profile(p store.Profile) *v1.Profile {
	out := &v1.Profile{
		ProfileId:   p.ID,
		Owner:       p.Owner,
		Pseudonym:   p.Pseudonym,
		Avatar:      p.Avatar,
		AgeCategory: p.AgeCategory,
		Country:     p.Country,
		PresentationStyle: &v1.PresentationStyle{Voicing: &v1.Voicing{
			Enabled: &p.VoicingEnabled,
			Speed:   voicingSpeeds[p.VoicingSpeed],
		}},
		DefaultMode:       modes[p.DefaultMode],
		InterfaceLanguage: p.InterfaceLanguage,
		AskBeforeShowing:  p.AskBeforeShowing,
		WorkStorage:       p.WorkStorage,
	}
	if p.Grade != nil {
		grade := int32(*p.Grade)
		out.Grade = &grade
	}
	if a := p.OpenAttempt; a != nil {
		out.OpenAttempt = &v1.OpenAttemptRef{AttemptId: a.ID}
		if a.LessonRunID != nil {
			out.OpenAttempt.Segment = &v1.SegmentPlace{LessonRunId: *a.LessonRunID, Segment: int32(*a.Segment)}
		}
	}
	for _, r := range p.OpenLessonRuns {
		out.OpenLessonRuns = append(out.OpenLessonRuns, &v1.LessonRunRef{LessonRunId: r.ID, Lesson: r.LessonID,
			PackageDigest: r.PackageDigest})
	}
	now := s.now()
	if p.Consent.Giver != "" {
		out.Consent = &v1.ProfileConsent{Giver: givers[p.Consent.Giver], Need: s.need(p.Consent, now)}
		if out.Consent.Need == v1.ConsentNeed_CONSENT_NEED_PARENT {
			out.Consent.Waiting = childConsent(p.Consent.ChildConsent)
		}
	}
	// The category of a child is refined a year after its age was given; the
	// owner's is their statement (privacy 3.3 spec).
	out.AgeCheckDue = !p.Owner && p.AgeStatedOn != nil && !now.UTC().Before(p.AgeStatedOn.AddDate(1, 0, 0))
	return out
}

// The codes of the schema, in the contract's enums. The schema's checks hold
// every code a row may have.
var (
	voicingSpeeds = map[string]v1.VoicingSpeed{
		"normal": v1.VoicingSpeed_VOICING_SPEED_NORMAL,
		"slow":   v1.VoicingSpeed_VOICING_SPEED_SLOW,
	}
	modes = map[string]v1.Mode{
		"by_steps": v1.Mode_MODE_BY_STEPS,
		"self":     v1.Mode_MODE_SELF,
	}
	consentStates = map[string]v1.ConsentState{
		"waiting":   v1.ConsentState_CONSENT_STATE_WAITING,
		"confirmed": v1.ConsentState_CONSENT_STATE_CONFIRMED,
	}
)
