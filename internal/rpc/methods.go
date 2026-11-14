package rpc

import (
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"

	"github.com/altnovatrail/api/internal/authz"
)

// rule is what the checks of architecture 3.1 ask of a method before its handler
// runs (R33).
type rule struct {
	// open is a method that goes with no token: an open view of a package, and
	// the legal texts.
	open bool
	// noAccount is the one method a UID with no account may call: it makes the
	// account.
	noAccount bool
	// right is the right on the request's profile, checked before anything is
	// read by its ID; empty, the method acts on the token's account.
	right authz.Right
	// fresh is an irreversible action, which waits for a fresh sign-in (R20).
	fresh bool
	// dropsDangling is the deletion of a profile: a tuple whose profile is gone —
	// a deletion before it that left the tuple — is deleted by its repeat, which
	// is told "no profile" all the same (R21; architecture 3.10).
	dropsDangling bool
}

// The rules' shapes, as the table of architecture 3.9 gives them.
var (
	openView  = rule{open: true}
	ofAccount = rule{}
	toLearn   = rule{right: authz.CanLearn}
	toManage  = rule{right: authz.CanManage}
	toView    = rule{right: authz.CanView}
	freshly   = rule{fresh: true}
	makeAnew  = rule{noAccount: true}
	deleting  = rule{right: authz.CanManage, fresh: true, dropsDangling: true}
)

// methods is every method of the contract and its rule. A method the server
// registers and this table lacks is served by no one: the checks refuse it, and
// a test holds the table to the server's methods.
//
// Eight methods take one of two rights, by who asks: the showing of a
// solution, the export of a profile, the storage of works and the listing,
// reading and erasing of a stored work are the learner's can_learn or the
// adult's can_manage, and the skill map and the share of the quota the
// learner's can_learn or the adult's can_view. The owner holds every right in
// the first slice; the table checks can_learn, and the handler the right its
// request names.
var methods = map[string]rule{
	apiv1connect.AccountServiceGetAccountProcedure:          ofAccount,
	apiv1connect.AccountServiceCreateAccountProcedure:       makeAnew,
	apiv1connect.AccountServiceSetPinProcedure:              ofAccount,
	apiv1connect.AccountServiceVerifyPinProcedure:           ofAccount,
	apiv1connect.AccountServiceChangePinProcedure:           ofAccount,
	apiv1connect.AccountServiceResetPinProcedure:            freshly,
	apiv1connect.AccountServiceCreateOwnerProfileProcedure:  ofAccount,
	apiv1connect.AccountServiceRequestChildConsentProcedure: ofAccount,
	apiv1connect.AccountServiceCreateChildProfileProcedure:  ofAccount,
	apiv1connect.AccountServiceUpdateProfileProcedure:       toManage,
	apiv1connect.AccountServiceSetVoicingProcedure:          toLearn,
	apiv1connect.AccountServiceDeleteProfileProcedure:       deleting,
	apiv1connect.AccountServiceDeleteAccountProcedure:       freshly,
	apiv1connect.AccountServiceExportProfileProcedure:       toLearn,
	apiv1connect.AccountServiceExportAccountProcedure:       ofAccount,
	apiv1connect.AccountServiceCheckOwnKeyProcedure:         toManage,
	apiv1connect.AccountServiceListOwnKeyProvidersProcedure: toManage,
	apiv1connect.AccountServiceSendConsentFormProcedure:     ofAccount,
	apiv1connect.AccountServiceCancelChildConsentProcedure:  ofAccount,
	apiv1connect.AccountServiceGiveOwnConsentProcedure:      toLearn,
	apiv1connect.AccountServiceWithdrawOwnConsentProcedure:  toLearn,
	apiv1connect.AccountServiceRestateAgeProcedure:          toManage,
	apiv1connect.AccountServiceSetWorkStorageProcedure:      toLearn,
	apiv1connect.AccountServiceGetQuotaProcedure:            toLearn,

	apiv1connect.AttemptServiceSubmitWorkProcedure:            toLearn,
	apiv1connect.AttemptServiceSubmitEssayProcedure:           toLearn,
	apiv1connect.AttemptServiceDoneProcedure:                  toLearn,
	apiv1connect.AttemptServiceGetRungWordsProcedure:          toLearn,
	apiv1connect.AttemptServiceGetCorrectAnswerWordsProcedure: toLearn,
	apiv1connect.AttemptServiceExplainDifferentlyProcedure:    toLearn,
	apiv1connect.AttemptServiceGetHintProcedure:               toLearn,
	apiv1connect.AttemptServiceBeginStepProcedure:             toLearn,
	apiv1connect.AttemptServiceShowSolutionProcedure:          toLearn,
	apiv1connect.AttemptServiceReportProblemProcedure:         toLearn,

	apiv1connect.LearningServiceChooseNextProcedure:     toLearn,
	apiv1connect.LearningServiceBeginAttemptProcedure:   toLearn,
	apiv1connect.LearningServiceGetOpenAttemptProcedure: toLearn,
	apiv1connect.LearningServiceOpenLessonProcedure:     toLearn,
	apiv1connect.LearningServiceGetLessonRunProcedure:   toLearn,
	apiv1connect.LearningServiceCompleteLessonProcedure: toLearn,

	apiv1connect.ModuleServiceListCatalogProcedure:          toLearn,
	apiv1connect.ModuleServiceConnectModuleProcedure:        toLearn,
	apiv1connect.ModuleServiceDisconnectModuleProcedure:     toLearn,
	apiv1connect.ModuleServiceListConnectedModulesProcedure: toLearn,

	apiv1connect.PackageServiceGetManifestProcedure:     openView,
	apiv1connect.PackageServiceGetSkillGraphProcedure:   openView,
	apiv1connect.PackageServiceGetTechniquesProcedure:   openView,
	apiv1connect.PackageServiceGetSolvingStepsProcedure: openView,
	apiv1connect.PackageServiceCheckPackagesProcedure:   ofAccount,

	apiv1connect.ProgressServiceGetSkillMapProcedure: toLearn,
	apiv1connect.ProgressServiceGetProgressProcedure: toView,

	apiv1connect.LegalServiceGetLegalTextsProcedure: openView,

	// Answered unimplemented until T75 of RUN.md, behind their checks (R69).
	apiv1connect.WorkServicePutWorkProcedure:    toLearn,
	apiv1connect.WorkServiceListWorksProcedure:  toLearn,
	apiv1connect.WorkServiceGetWorkProcedure:    toLearn,
	apiv1connect.WorkServiceDeleteWorkProcedure: toLearn,
}
