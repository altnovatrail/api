package server_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/altnovatrail/spec/contracts/novatrail/api/v1/apiv1connect"

	"github.com/altnovatrail/api/internal/catalog"
	"github.com/altnovatrail/api/internal/rpc"
	"github.com/altnovatrail/api/internal/server"
)

// connectNotFound is the body of http.NotFound, by which Connect answers a
// request for no method of a service that is not of its protocol: it repeats
// nothing of the request.
const connectNotFound = "404 page not found\n"

// Whatever path and query a request names, the routes answer it without a
// redirect, which would repeat them in its Location and its body: what is no
// route is not found, with no body or with connectNotFound. The mount is
// serve's — rpc.Mount, with a subtree for each service of the contract, and
// the pattern of the files of the device parts —, on services that answer
// every method unimplemented.
func FuzzRoutes(f *testing.F) {
	for _, seed := range [][2]string{
		{"/health", ""},
		{"//health", ""},
		{"/learner/../health", "a=b"},
		{"/novatrail.api.v1.AccountService/GetAccount", ""},
		{"/novatrail.api.v1.AccountService", "learner=x"},
		{"/novatrail.api.v1.LearningService/NoSuchMethod", "connect=v1"},
		{"/packages/sha256-0/device/media/a.png", ""},
		{"/packages/sha256-0/device", ""},
		{"/packages/sha256-0/device/", ""},
		{"/packages/sha256-0//device/a", ""},
		{"", ""},
		{"health", ""},
	} {
		f.Add(seed[0], seed[1])
	}
	services := connect.NewServer()
	apiv1connect.RegisterAccountServiceHandler(services, apiv1connect.UnimplementedAccountServiceHandler{})
	apiv1connect.RegisterAttemptServiceHandler(services, apiv1connect.UnimplementedAttemptServiceHandler{})
	apiv1connect.RegisterLearningServiceHandler(services, apiv1connect.UnimplementedLearningServiceHandler{})
	apiv1connect.RegisterModuleServiceHandler(services, apiv1connect.UnimplementedModuleServiceHandler{})
	apiv1connect.RegisterPackageServiceHandler(services, apiv1connect.UnimplementedPackageServiceHandler{})
	apiv1connect.RegisterProgressServiceHandler(services, apiv1connect.UnimplementedProgressServiceHandler{})
	apiv1connect.RegisterLegalServiceHandler(services, apiv1connect.UnimplementedLegalServiceHandler{})
	routes := server.Routes(slog.New(slog.DiscardHandler), "", func(mux *http.ServeMux) {
		rpc.Mount(mux, services)
		mux.Handle(catalog.FilesPattern, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	})
	f.Fuzz(func(t *testing.T, path, query string) {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.URL.Path, request.URL.RawQuery = path, query
		answer := httptest.NewRecorder()
		routes.ServeHTTP(answer, request)
		if location := answer.Header().Get("Location"); location != "" || answer.Code/100 == 3 {
			t.Fatalf("GET %q ? %q = %d to %q, a redirect", path, query, answer.Code, location)
		}
		if answer.Code == http.StatusNotFound && answer.Body.Len() != 0 && answer.Body.String() != connectNotFound {
			t.Fatalf("GET %q ? %q = 404 with %q", path, query, answer.Body)
		}
	})
}
