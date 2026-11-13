package main

import (
	"context"
	"io"
	"log/slog"

	"github.com/altnovatrail/api/internal/admission"
	"github.com/altnovatrail/api/internal/config"
	"github.com/altnovatrail/api/internal/logging"
)

// check checks what the deployment declares for admission to AI and serves
// nothing (R45; the amendment of 2026-11-08 to R106 spec): the file of facts by
// the admission table of this release, and the least stage; and a copy's folder
// of legal texts by the rules of the part, and the store of the forms (R52). The rollout of
// infra runs it as a Cloud Run job of the same image before the migrations, so
// that a dictionary of facts that this release does not know stops the rollout
// before anything changed, rather than the first start of the revision after
// them. A refusal is the configuration's, printed before any log as every
// refusal of the configuration is; what it found, the line admission says.
func check(_ context.Context, args, environ []string, stdout, stderr io.Writer) error {
	if err := flags("check", args, stderr); err != nil {
		return err
	}
	cfg, err := config.LoadFrom(environ)
	if err != nil {
		return err
	}
	declared, err := config.AdmissionFrom(environ)
	if err != nil {
		return err
	}
	_, report, err := admission.Load(declared)
	if err != nil {
		return err
	}
	// A copy's folder of legal texts is checked by the rules of the part, as
	// serve reads it at its start, and the store of the forms named as serve
	// takes it (R52).
	agreements, err := config.AgreementsFrom(environ)
	if err != nil {
		return err
	}
	if _, err := legalTexts(agreements); err != nil {
		return err
	}
	logAdmission(logging.New(stdout, cfg.LogLevel), report, agreements)
	return nil
}

// logAdmission writes what the check found the deployment to
// declare for admission: what is missing, without a provider, a fact or a path.
func logAdmission(log *slog.Logger, r admission.Report, a config.Agreements) {
	log.Info(logging.EventAdmission,
		logging.FieldAdmissionTable, r.TableVersion,
		logging.FieldFactsFile, r.FactsFile,
		logging.FieldRung, r.Rung,
		logging.FieldRungSet, r.RungSet,
		logging.FieldPlatformKeys, r.PlatformKeys,
		logging.FieldTutorStub, r.Stub,
		logging.FieldLegalDir, a.LegalDir != "",
		logging.FieldConsentForms, a.Forms(),
	)
}
