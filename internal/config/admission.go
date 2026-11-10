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
)

// The stages of admission, as tutor codes them.
const (
	RungAuthor = "author"
	RungFull   = "full"
)

func init() {
	declared[VarAdmissionFactsFile] = struct{}{}
	declared[VarAdmissionRung] = struct{}{}
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
	if len(problems) > 0 {
		return Admission{}, errors.Join(problems...)
	}
	return cfg, nil
}
