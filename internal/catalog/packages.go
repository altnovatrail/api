package catalog

import (
	"context"

	"connectrpc.com/connect/v2"
	v1 "github.com/altnovatrail/spec/contracts/novatrail/api/v1"
	"github.com/altnovatrail/spec/types/formatv0"

	"github.com/altnovatrail/api/internal/packages"
	"github.com/altnovatrail/api/internal/rpc"
)

// PackageService gives what a package gives by its digest (R62 spec; contract,
// PackageService): the four open views, which go without a token by Connect GET
// and are cached for a day, and which digests the image still gives.
type PackageService struct {
	c *Catalog
}

// Packages is the PackageService of the catalog.
func (c *Catalog) Packages() *PackageService { return &PackageService{c: c} }

// GetManifest gives the open view of the package's manifest.
func (s *PackageService) GetManifest(ctx context.Context, req *v1.GetManifestRequest) (*v1.GetManifestResponse, error) {
	v, err := s.c.given(ctx, req.GetPackageDigest())
	if err != nil {
		return nil, err
	}
	return &v1.GetManifestResponse{Manifest: v.manifest}, nil
}

// GetSkillGraph gives the open view of the package's skill graph, with the tier
// of each skill: an answer without a view when the module has no graph.
func (s *PackageService) GetSkillGraph(ctx context.Context, req *v1.GetSkillGraphRequest) (*v1.GetSkillGraphResponse, error) {
	v, err := s.c.given(ctx, req.GetPackageDigest())
	if err != nil {
		return nil, err
	}
	return &v1.GetSkillGraphResponse{SkillGraph: v.skillGraph, SkillTiers: v.tiers}, nil
}

// GetTechniques gives the open view of the package's technique catalog.
func (s *PackageService) GetTechniques(ctx context.Context, req *v1.GetTechniquesRequest) (*v1.GetTechniquesResponse, error) {
	v, err := s.c.given(ctx, req.GetPackageDigest())
	if err != nil {
		return nil, err
	}
	return &v1.GetTechniquesResponse{Techniques: v.techniques}, nil
}

// GetSolvingSteps gives the open view of the package's solving steps.
func (s *PackageService) GetSolvingSteps(ctx context.Context, req *v1.GetSolvingStepsRequest) (*v1.GetSolvingStepsResponse, error) {
	v, err := s.c.given(ctx, req.GetPackageDigest())
	if err != nil {
		return nil, err
	}
	return &v1.GetSolvingStepsResponse{SolvingSteps: v.solvingSteps}, nil
}

// CheckPackages gives each digest of the request, in its order, with whether the
// image gives it. api keeps none of them (contract, CheckPackages).
func (s *PackageService) CheckPackages(_ context.Context, req *v1.CheckPackagesRequest) (*v1.CheckPackagesResponse, error) {
	checks := make([]*v1.PackageCheck, 0, len(req.GetPackageDigests()))
	for _, digest := range req.GetPackageDigests() {
		if !packages.IsDigest(digest) {
			return nil, notADigest()
		}
		checks = append(checks, &v1.PackageCheck{PackageDigest: digest, Status: statusOf(s.c.set.Status(formatv0.Digest(digest)))})
	}
	return &v1.CheckPackagesResponse{Packages: checks}, nil
}

// given is the open views of the package of digest, when the image gives it, and
// the answer is then kept for a day; otherwise the refusal, kept by nobody.
func (c *Catalog) given(ctx context.Context, digest string) (views, error) {
	cache(ctx, noStore)
	if !packages.IsDigest(digest) {
		return views{}, notADigest()
	}
	switch c.set.Status(formatv0.Digest(digest)) {
	case packages.Unknown:
		return views{}, rpc.Refuse(ctx, connect.CodeUnavailable, v1.Cause_CAUSE_PACKAGE_UNKNOWN)
	case packages.NotGiven:
		return views{}, rpc.Refuse(ctx, connect.CodeFailedPrecondition, v1.Cause_CAUSE_VERSION_NOT_GIVEN)
	}
	cache(ctx, cacheFor)
	return c.views[formatv0.Digest(digest)], nil
}

// cache sets how long the answer of the request is kept.
func cache(ctx context.Context, control string) {
	if info, ok := connect.CallInfoForServerContext(ctx); ok {
		info.ResponseHeader().Set("Cache-Control", control)
	}
}

// notADigest is a digest that is not one: a form the contract refuses, with no
// Refusal.
func notADigest() error {
	return connect.NewError(connect.CodeInvalidArgument, "the package digest is not sha256: and 64 lowercase hexadecimal digits")
}

// statusOf is the status in the contract's words.
func statusOf(status packages.Status) v1.PackageStatus {
	switch status {
	case packages.Given:
		return v1.PackageStatus_PACKAGE_STATUS_GIVEN
	case packages.NotGiven:
		return v1.PackageStatus_PACKAGE_STATUS_NOT_GIVEN
	}
	return v1.PackageStatus_PACKAGE_STATUS_UNKNOWN
}
