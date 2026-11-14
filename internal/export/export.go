// Package export is api's part of the export of a profile or an account, api.zip
// (R52; R135 spec): its files by spec's part export/ — account.json in the
// export of the account, a file of each profile and the .proto files the records
// of protobuf are written by —, of whatever the database api keeps, with what
// the package of a module tells of an attempt while the image holds it. The
// evidence, the state and the observation go in as the JSON of their messages,
// with the fields' names as in their .proto files, which go in beside them.
package export

import (
	"archive/zip"
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"time"

	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	observationv1 "github.com/altnovatrail/spec/contracts/novatrail/observation/v1"
	"github.com/altnovatrail/spec/types/exportv2"
	"github.com/altnovatrail/spec/types/formatv0"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	learnermodel "github.com/altnovatrail/learner-model"

	"github.com/altnovatrail/api/internal/model"
	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/store"
)

// files is the .proto files the export carries and the descriptors of
// learner-model's, built of them: those of the release file of the version of
// learner-model this binary runs, which release-verify accepted (just
// learner-model-proto), and the observation's of the contracts of spec's tag.
//
//go:embed proto learnermodel.binpb
var files embed.FS

// Version is the version of the part export/ api writes, the string
// ExportProfile and ExportAccount name.
var Version = strconv.Itoa(store.ExportFormat)

// The names of the files of api.zip (export/README.md of spec, section 3).
const (
	accountFile = "account.json"
	protoDir    = "proto"
	// observationProto is the .proto of the observation, which an export with a
	// submission carries.
	observationProto = "proto/novatrail/observation/v1/observation.proto"
)

// protoJSON is protobuf's JSON mapping with the fields' names of the .proto
// files.
var protoJSON = protojson.MarshalOptions{UseProtoNames: true}

// Exporter writes the export of the packages of an image and the model.
type Exporter struct {
	set      *packages.Set
	model    *model.Model
	evidence protoreflect.MessageType
	state    protoreflect.MessageType
}

// New is the exporter of the packages of set and the model m.
func New(set *packages.Set, m *model.Model) (*Exporter, error) {
	data, err := fs.ReadFile(files, "learnermodel.binpb")
	if err != nil {
		return nil, fmt.Errorf("export: read the descriptors: %w", err)
	}
	var set2 descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(data, &set2); err != nil {
		return nil, fmt.Errorf("export: read the descriptors: %w", err)
	}
	registry, err := protodesc.NewFiles(&set2)
	if err != nil {
		return nil, fmt.Errorf("export: read the descriptors: %w", err)
	}
	x := &Exporter{set: set, model: m}
	for name, into := range map[protoreflect.FullName]*protoreflect.MessageType{
		"novatrail.learnermodel.v1.Evidence": &x.evidence,
		"novatrail.learnermodel.v1.State":    &x.state,
	} {
		d, err := registry.FindDescriptorByName(name)
		if err != nil {
			return nil, fmt.Errorf("export: the descriptors have no %s: %w", name, err)
		}
		md, ok := d.(protoreflect.MessageDescriptor)
		if !ok {
			return nil, fmt.Errorf("export: %s is no message", name)
		}
		*into = dynamicpb.NewMessageType(md)
	}
	return x, nil
}

// Profile is api.zip of the export of the account's profile, read in the
// snapshot of tx, or store.ErrNoProfile.
func (x *Exporter) Profile(ctx context.Context, tx pgx.Tx, accountID, profileID string) ([]byte, error) {
	p, submissions, err := x.profile(ctx, tx, accountID, profileID)
	if err != nil {
		return nil, err
	}
	return x.zip(nil, []exportv2.Profile{p}, submissions)
}

// Account is api.zip of the export of the account and every profile of it.
func (x *Exporter) Account(ctx context.Context, tx pgx.Tx, accountID string) ([]byte, error) {
	a, err := store.ExportAccount(ctx, tx, accountID)
	if err != nil {
		return nil, err
	}
	var profiles []exportv2.Profile
	submissions := false
	for _, id := range a.ProfileIds {
		p, sent, err := x.profile(ctx, tx, accountID, string(id))
		if err != nil {
			return nil, err
		}
		profiles, submissions = append(profiles, p), submissions || sent
	}
	return x.zip(&a, profiles, submissions)
}

// profile is the file of the profile, and whether it has a submission.
func (x *Exporter) profile(ctx context.Context, tx pgx.Tx, accountID, profileID string) (exportv2.Profile, bool, error) {
	e, err := store.ExportProfile(ctx, tx, accountID, profileID)
	if err != nil {
		return exportv2.Profile{}, false, err
	}
	p := e.Profile
	submissions := false
	for _, a := range e.Attempts {
		attempt, err := x.attempt(ctx, a, e.Observations[string(a.AttemptID)])
		if err != nil {
			return exportv2.Profile{}, false, err
		}
		submissions = submissions || len(attempt.Submissions) > 0
		p.Attempts = append(p.Attempts, attempt)
	}
	for i, data := range e.Evidence {
		if p.Evidence[i].Evidence, err = x.json(x.evidence, data); err != nil {
			return exportv2.Profile{}, false, err
		}
	}
	state := e.State
	if state == nil {
		// A profile with no state yet has the empty one, the fold of no evidence.
		if state, err = learnermodel.EncodeState(learnermodel.State{Kind: learnermodel.StateKind}); err != nil {
			return exportv2.Profile{}, false, fmt.Errorf("export: the empty state: %w", err)
		}
	}
	if p.State.State, err = x.json(x.state, state); err != nil {
		return exportv2.Profile{}, false, err
	}
	p.State.Modules = x.modules(p.Evidence)
	for _, w := range e.Works {
		work, err := storedWork(w)
		if err != nil {
			return exportv2.Profile{}, false, err
		}
		p.Works = append(p.Works, work)
	}
	return p, submissions, nil
}

// storedWork is the stored work w as the export writes it, as GetWork gives it
// (R70; contracts/README.md of spec, section 15): its history and the context
// of each submission as the JSON their bytes hold — PutWork took no other —,
// and its picture in base64.
func storedWork(w store.ExportedWork) (exportv2.Work, error) {
	var stored v1.StoredWork
	if err := proto.Unmarshal(w.Bytes, &stored); err != nil {
		return exportv2.Work{}, fmt.Errorf("export: read a work: %w", err)
	}
	out := w.Work
	out.History = json.RawMessage(stored.GetHistory())
	out.Submissions = []exportv2.WorkSubmission{}
	out.Picture = base64.StdEncoding.EncodeToString(stored.GetPicture())
	for _, s := range stored.GetSubmissions() {
		out.Submissions = append(out.Submissions, exportv2.WorkSubmission{Context: json.RawMessage(s.GetContext()),
			HistoryLength: int64(s.GetHistoryLength())})
	}
	return out, nil
}

// modules is each module of the log with the version whose graph the state
// was computed by: the current version of the image, or none (R08).
func (x *Exporter) modules(journal []exportv2.EvidenceEntry) []exportv2.StateEntryModule {
	var ids []string
	for _, e := range journal {
		if !slices.Contains(ids, e.ModuleID) {
			ids = append(ids, e.ModuleID)
		}
	}
	slices.Sort(ids)
	modules := []exportv2.StateEntryModule{}
	for _, id := range ids {
		m := exportv2.StateEntryModule{ModuleID: id}
		if pkg, ok := x.set.Current(formatv0.ModuleID(id)); ok {
			version := string(pkg.Inventory.Version)
			m.ModuleVersion = &version
		}
		modules = append(modules, m)
	}
	return modules
}

// attempt is the attempt a, with what its submissions and its package tell.
func (x *Exporter) attempt(ctx context.Context, a store.ExportedAttempt, observations map[int64][]byte) (exportv2.Attempt, error) {
	out := a.Attempt
	told, err := x.model.Tell(ctx, a.Owed)
	if err != nil {
		return exportv2.Attempt{}, err
	}
	out.Traps = told.Traps
	if a.Owed.StepTechniques {
		out.StepTechniqueIds = told.Techniques
	}
	if a.Owed.RequestedTechniques {
		out.RequestedTechniqueIds = told.Techniques
	}
	if a.LeadingQuestionGiven && told.Question != "" {
		out.RequestedLeadingQuestion = &told.Question
	}
	for _, item := range told.Items {
		out.Items = append(out.Items, attemptItem(item))
	}
	for i, answer := range a.Owed.Answers {
		s, err := x.submission(out.Submissions[i], answer, observations[out.Submissions[i].Number])
		if err != nil {
			return exportv2.Attempt{}, err
		}
		out.Submissions[i] = s
	}
	return out, nil
}

// submission is the submission s with what its answer and its observation
// hold: the verdict of each item and answer slot, what was read, the cause of
// its outcome, and the taps, which the answer of a submission of taps alone
// holds as its values.
func (x *Exporter) submission(s exportv2.Submission, answer, observation []byte) (exportv2.Submission, error) {
	var res v1.SubmitWorkResponse
	if err := proto.Unmarshal(answer, &res); err != nil {
		return exportv2.Submission{}, fmt.Errorf("export: read a submission: %w", err)
	}
	s.Items, s.TappedChoices, s.WrongAnswers = []exportv2.SubmissionItem{}, []exportv2.SubmissionTappedChoice{}, int64(res.GetWrongAnswers())
	if res.GetOutcome() != nil && res.GetOutcome().GetCause() != v1.Cause_CAUSE_UNSPECIFIED {
		s.Outcome = &exportv2.SubmissionOutcome{Cause: res.GetOutcome().GetCause().String()}
	}
	for _, item := range res.GetItems() {
		out := exportv2.SubmissionItem{Verdict: exportv2.SubmissionItemVerdict(item.GetVerdict()),
			AnswerSlots: []exportv2.SubmissionItemAnswerSlot{}}
		for _, slot := range item.GetAnswerSlots() {
			place, err := protoJSON.Marshal(slot.GetAnswerSlot())
			if err != nil {
				return exportv2.Submission{}, fmt.Errorf("export: write an answer slot: %w", err)
			}
			at := exportv2.SubmissionItemAnswerSlot{AnswerSlot: place,
				Verdict: exportv2.SubmissionItemAnswerSlotVerdict(slot.GetVerdict())}
			if r := slot.GetReason(); r != "" {
				reason := exportv2.SubmissionItemAnswerSlotReason(r)
				at.Reason = &reason
			}
			if slot.Value != nil {
				value := slot.GetValue()
				at.Value = &value
				if observation == nil {
					s.TappedChoices = append(s.TappedChoices, exportv2.SubmissionTappedChoice{AnswerSlot: place, Value: value})
				}
			}
			out.AnswerSlots = append(out.AnswerSlots, at)
		}
		s.Items = append(s.Items, out)
	}
	if observation != nil {
		var o observationv1.Observation
		if err := proto.Unmarshal(observation, &o); err != nil {
			return exportv2.Submission{}, fmt.Errorf("export: read an observation: %w", err)
		}
		data, err := protoJSON.Marshal(&o)
		if err != nil {
			return exportv2.Submission{}, fmt.Errorf("export: write an observation: %w", err)
		}
		raw := json.RawMessage(data)
		s.Observation = &raw
	}
	return s, nil
}

// verdicts is an item's verdict of learner-model, in the export's words, and
// what an item that gave none got, by its last reason.
var (
	verdicts = map[learnermodel.Verdict]string{learnermodel.VerdictCorrect: "correct", learnermodel.VerdictNotYet: "not_yet"}
	reasons  = map[learnermodel.UnverdictedReason]exportv2.AttemptItemUnverdictedReasons{
		learnermodel.UnverdictedUnreadableBlank: exportv2.AttemptItemUnverdictedReasonsUnreadableBlank,
		learnermodel.UnverdictedUnreadableOther: exportv2.AttemptItemUnverdictedReasonsUnreadableOther,
		learnermodel.UnverdictedUnchecked:       exportv2.AttemptItemUnverdictedReasonsUnchecked,
	}
)

// attemptItem is what came of an item, as the export writes it: an item with
// no verdict of correct or not yet has its last reason's — unreadable, or
// unchecked, which an item never submitted is too.
func attemptItem(item learnermodel.ItemAnswer) exportv2.AttemptItem {
	out := exportv2.AttemptItem{Place: int64(item.Place), Submitted: item.Submitted,
		UnverdictedReasons:             []exportv2.AttemptItemUnverdictedReasons{},
		WrongAnswersBeforeFirstVerdict: int64(item.WrongBeforeFirst), WrongAnswersBeforeFirstCorrect: int64(item.WrongBeforeFirstCorrect)}
	none := "unchecked"
	for _, r := range item.Unverdicted {
		out.UnverdictedReasons = append(out.UnverdictedReasons, reasons[r])
		if r != learnermodel.UnverdictedUnchecked {
			none = "unreadable"
		} else {
			none = "unchecked"
		}
	}
	out.FirstVerdict, out.FinalVerdict = exportv2.AttemptItemFirstVerdict(none), exportv2.AttemptItemFinalVerdict(none)
	if item.HasFirstVerdict {
		out.FirstVerdict = exportv2.AttemptItemFirstVerdict(verdicts[item.FirstVerdict])
	}
	if item.HasFinalVerdict {
		out.FinalVerdict = exportv2.AttemptItemFinalVerdict(verdicts[item.FinalVerdict])
	}
	return out
}

// json is the JSON of the bytes of a message of the type t.
func (x *Exporter) json(t protoreflect.MessageType, data []byte) (json.RawMessage, error) {
	m := t.New().Interface()
	if err := proto.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("export: read %s: %w", t.Descriptor().FullName(), err)
	}
	out, err := protoJSON.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("export: write %s: %w", t.Descriptor().FullName(), err)
	}
	return out, nil
}

// zip is api.zip of the account, when it is the account's export, its
// profiles, and the .proto files: learner-model's, as every profile has its
// state, and the observation's when a submission is there. Its entries are
// deflated, in the order of their names, at the same time, so that one export
// is the same bytes.
func (x *Exporter) zip(account *exportv2.Account, profiles []exportv2.Profile, submissions bool) ([]byte, error) {
	entries := map[string][]byte{}
	if account != nil {
		data, err := json.Marshal(account)
		if err != nil {
			return nil, fmt.Errorf("export: write the account: %w", err)
		}
		entries[accountFile] = data
	}
	for _, p := range profiles {
		data, err := json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("export: write a profile: %w", err)
		}
		entries["profiles/"+string(p.ProfileID)+".json"] = data
	}
	err := fs.WalkDir(files, protoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || (path == observationProto && !submissions) {
			return err
		}
		entries[path], err = fs.ReadFile(files, path)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("export: read the .proto files: %w", err)
	}
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		f, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			return nil, fmt.Errorf("export: write %s: %w", name, err)
		}
		if _, err := f.Write(entries[name]); err != nil {
			return nil, fmt.Errorf("export: write %s: %w", name, err)
		}
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("export: close the zip: %w", err)
	}
	return buf.Bytes(), nil
}
