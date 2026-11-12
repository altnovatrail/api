package config

import (
	"errors"
	"fmt"
	"path/filepath"
)

// The variables of admission to AI (R25, R45): what the deployment declares,
// beside its rollout list in infra, and the first app in a file of the
// author's own (the amendment of 2026-11-08 to R106 spec).
const (
	// VarAdmissionFactsFile is the absolute path of the file of the deployment's
	// facts (the admission text of spec, section 5), as the path of a secret's
	// file is given; on Cloud Run, a volume of an exact version of a secret. Its
	// type and how it is read are tutor's.
	VarAdmissionFactsFile = "API_ADMISSION_FACTS_FILE"
	// VarAdmissionRung is the least stage of tutor's rule of admission of
	// models, which api hands to Table.Select: "author" only in the first app,
	// "full" anywhere else.
	VarAdmissionRung = "API_ADMISSION_RUNG"
	// VarAnthropicKey is the platform's key of Anthropic, by which platform AI
	// calls its models: a secret, read from the variable or from the file its
	// twin with the suffix _FILE names — on Cloud Run, a volume of a secret of
	// Secret Manager (R11, R46). Unset, platform AI has no route to Anthropic.
	VarAnthropicKey = "API_ANTHROPIC_KEY"
	// VarOpenAIKey was the platform's key of OpenAI. api gives tutor no adapter
	// of OpenAI, whose models the admission table admits for nothing, so the
	// variable, or its twin _FILE, set is a refusal: a deployment that still
	// passes it learns so at its start rather than holding a key nothing calls
	// (R54).
	VarOpenAIKey = "API_OPENAI_KEY"
)

// The stages of admission, as tutor codes them.
const (
	RungAuthor = "author"
	RungFull   = "full"
)

func init() {
	declared[VarAdmissionFactsFile] = struct{}{}
	declared[VarAdmissionRung] = struct{}{}
	for _, name := range []string{VarAnthropicKey, VarOpenAIKey} {
		declared[name] = struct{}{}
		declared[name+fileSuffix] = struct{}{}
	}
}

// Admission is what the deployment declares for the function of admission.
type Admission struct {
	// FactsFile is the absolute path of the file of facts, or empty for none:
	// no fact then holds, and platform AI is closed.
	FactsFile string
	// Rung is the least stage, RungAuthor or RungFull.
	Rung string
	// RungSet says whether the deployment named the stage; unset, it is
	// RungFull, closed by default as the function of admission is.
	RungSet bool
	// Keys are the platform's keys of the providers, each empty when unset.
	Keys PlatformKeys
}

// AdmissionFrom reads what the deployment declares for admission from a set of
// KEY=VALUE strings, and validates it. Deployed — K_SERVICE set, as serve runs
// on Cloud Run, or CLOUD_RUN_JOB, as api check runs before the migrations — the
// stage "author" is refused: it is the first app's alone, where the author alone
// learns, and any deployment, even one where the author alone learns, passes
// "full" (R25). Only Cloud Run's variables tell a deployment here: one of its own
// elsewhere passes "full" by its own rule, as it keeps its own facts (R25).
// The file of facts holds no secret, but it lies outside any git working tree
// as a secret's file does: the first app's is the author's own, outside the
// repository.
func AdmissionFrom(environ []string) (Admission, error) {
	env := read(environ)
	deployedBy := ""
	for _, name := range []string{VarService, VarJob} {
		if _, set := env[name]; set && deployedBy == "" {
			deployedBy = name
		}
	}
	rung, set := env[VarAdmissionRung]
	cfg := Admission{FactsFile: env[VarAdmissionFactsFile], Rung: rung, RungSet: set}
	if !set {
		cfg.Rung = RungFull
	}

	var problems []error
	switch path := cfg.FactsFile; {
	case path == "":
	case !filepath.IsAbs(path):
		problems = append(problems, fmt.Errorf("%w: %s must be an absolute path, got %q", ErrInvalid, VarAdmissionFactsFile, path))
	case inWorkingTree(path):
		problems = append(problems, fmt.Errorf("%w: %s names a file in a git working tree, %q: keep it outside any repository",
			ErrInvalid, VarAdmissionFactsFile, path))
	}
	switch {
	case cfg.Rung != RungAuthor && cfg.Rung != RungFull:
		problems = append(problems, fmt.Errorf("%w: %s must be %s or %s, got %q", ErrInvalid, VarAdmissionRung, RungAuthor, RungFull, cfg.Rung))
	case cfg.Rung == RungAuthor && deployedBy != "":
		problems = append(problems, fmt.Errorf("%w: %s must not be %s when %s is set: the stage %s is the first app's alone",
			ErrInvalid, VarAdmissionRung, RungAuthor, deployedBy, RungAuthor))
	}
	_, inline := env[VarAnthropicKey]
	_, fromFile := env[VarAnthropicKey+fileSuffix]
	if inline || fromFile {
		key, err := env.secret(VarAnthropicKey)
		if err != nil {
			problems = append(problems, err)
		}
		cfg.Keys.Anthropic = key
	}
	for _, name := range []string{VarOpenAIKey, VarOpenAIKey + fileSuffix} {
		if _, set := env[name]; set {
			problems = append(problems, fmt.Errorf("%w: %s is set, and api gives tutor no adapter of OpenAI: unset it",
				ErrInvalid, name))
		}
	}
	if len(problems) > 0 {
		return Admission{}, errors.Join(problems...)
	}
	return cfg, nil
}

// PlatformKeys are the platform's keys of the providers whose models platform AI
// calls (R11, R46): Anthropic's alone, the one provider api gives tutor an
// adapter of (R54). It is optional, since a provider with no key is no route of
// platform AI — tutor's Choose takes none of its models.
type PlatformKeys struct {
	Anthropic Secret
}

// ByProvider is each key set, by its provider's code in the admission table:
// the one list of the providers whose keys the platform may hold.
func (k PlatformKeys) ByProvider() map[string]Secret {
	keys := map[string]Secret{}
	for provider, s := range map[string]Secret{"anthropic": k.Anthropic} {
		if s.Reveal() != "" {
			keys[provider] = s
		}
	}
	return keys
}
