package config

import (
	"errors"
	"fmt"
)

// VarJob is the name of the Cloud Run job being run, which Cloud Run sets in
// every task of a job: deployed, sweep runs as one, and the variable says so,
// as K_SERVICE says it of serve.
const VarJob = "CLOUD_RUN_JOB"

func init() { declared[VarJob] = struct{}{} }

// Sweep is what the daily job sweep opens (R21, R38): what serve opens but the
// origin of app. The level of its log and the longest a request may run — a mark
// or a tuple younger than that may be a request's still going — are the
// process's Config, read as serve reads them, so that a deployment that gives
// both the same variables gives them the same values.
type Sweep struct {
	// DatabaseURL is the pooler's connection string of the database api.
	DatabaseURL Secret
	// OpenFGADatabaseURL is the pooler's connection string of the database
	// openfga.
	OpenFGADatabaseURL Secret
	// FirebaseProjectID is the Firebase project whose users the job lists.
	FirebaseProjectID string
	// FirebaseCredentials is a service account's key off a deployment;
	// deployed, none (R32).
	FirebaseCredentials Secret
	// Job is the name of the Cloud Run job, or empty off a deployment.
	Job string
	// PackagesDir is the absolute path of the folder of the packages, as serve
	// reads it: the evidence the job writes is built of them (R49).
	PackagesDir string
}

// Deployed reports whether the job runs as a deployed Cloud Run job.
func (s *Sweep) Deployed() bool { return s.Job != "" }

// SweepFrom reads what sweep opens from a set of KEY=VALUE strings, as
// ServeFrom reads what serve opens, and validates it.
func SweepFrom(environ []string) (*Sweep, error) {
	env := read(environ)

	var problems []error
	cfg := &Sweep{FirebaseProjectID: env[VarFirebaseProjectID], Job: env[VarJob]}
	// In this order, so that the refusals read the same on every run.
	problems = append(problems, databaseURLs(env, []namedSecret{
		{VarDatabaseURL, &cfg.DatabaseURL},
		{VarOpenFGADatabaseURL, &cfg.OpenFGADatabaseURL},
	})...)
	if cfg.FirebaseProjectID == "" {
		problems = append(problems, fmt.Errorf("%w: %s must be set", ErrInvalid, VarFirebaseProjectID))
	}
	if err := firebaseCredentials(env, cfg.Deployed(), VarJob, &cfg.FirebaseCredentials); err != nil {
		problems = append(problems, err)
	}
	var err error
	if cfg.PackagesDir, err = packagesDir(env); err != nil {
		problems = append(problems, err)
	}
	if len(problems) > 0 {
		return nil, errors.Join(problems...)
	}
	return cfg, nil
}
