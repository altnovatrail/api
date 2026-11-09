package rpc

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/altnovatrail/api/internal/authn"
	"github.com/altnovatrail/api/internal/logging"
)

// The table has every method the server registers and no other, and a method
// with a right is one whose request names a profile: a right with no profile to
// check it on would refuse every request, and a profile with no right checked
// would be read before its right.
func TestTheTableIsTheContracts(t *testing.T) {
	t.Parallel()

	server := NewServer(Deps{}, Handlers{})
	var registered []string
	for spec := range server.Specs() {
		registered = append(registered, spec.Procedure)
		r, known := methods[spec.Procedure]
		if !known {
			t.Errorf("%s has no rule", spec.Procedure)
			continue
		}
		method, ok := spec.Schema.(protoreflect.MethodDescriptor)
		if !ok {
			t.Fatalf("%s has no descriptor", spec.Procedure)
		}
		names := method.Input().Fields().ByName("profile_id") != nil
		if names != (r.right != "") {
			t.Errorf("%s: request names a profile = %v, rule's right = %q", spec.Procedure, names, r.right)
		}
		if r.open != (spec.IdempotencyLevel == connect.IdempotencyNoSideEffects) {
			t.Errorf("%s: open = %v, but its idempotency is %v", spec.Procedure, r.open, spec.IdempotencyLevel)
		}
	}
	for procedure := range methods {
		if !slices.Contains(registered, procedure) {
			t.Errorf("%s is in the table and not on the server", procedure)
		}
	}
	for procedure, r := range methods {
		if r.noAccount && procedure != "/novatrail.api.v1.AccountService/CreateAccount" {
			t.Errorf("%s goes with no account: only CreateAccount does", procedure)
		}
	}
}

// What a request's line writes — every method, code, cause and class there is —
// passes the log's guard, so that none of them is ever dropped.
func TestEveryValueOfARequestsLineIsLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logging.New(&buf, slog.LevelInfo)
	for procedure := range methods {
		log.Info(logging.EventRequest, logging.FieldMethod, procedure)
	}
	for code := connect.Code(1); code <= connect.CodeUnauthenticated; code++ {
		log.Info(logging.EventRequest, logging.FieldCode, code.String())
	}
	log.Info(logging.EventRequest, logging.FieldCode, "ok")
	for number := range v1.Cause_name {
		log.Info(logging.EventRequest, logging.FieldCause, v1.Cause(number).String())
	}
	for class := authn.Expired; class <= authn.Failed; class++ {
		log.Info(logging.EventRequest, logging.FieldTokenClass, class.String())
	}
	if strings.Contains(buf.String(), "dropped_fields") {
		t.Errorf("the guard dropped a value of a request's line:\n%s", buf.String())
	}
	if len(logging.TokenClasses) != int(authn.Failed) || len(logging.Codes) != int(connect.CodeUnauthenticated)+1 {
		t.Errorf("the log's sets are not authn's classes and Connect's codes: %v, %v", logging.TokenClasses, logging.Codes)
	}
	// And a value that is none of them is dropped: a method's line holds a name
	// of the contract, never what a request put in its path.
	buf.Reset()
	log.Info(logging.EventRequest, logging.FieldMethod, "/novatrail.api.v1.AccountService/uid-123")
	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil || line["dropped_fields"] == nil {
		t.Errorf("a method of no contract's shape was logged: %s", buf.String())
	}
}
