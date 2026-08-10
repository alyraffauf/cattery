package add

import (
	"path/filepath"
	"strings"

	"github.com/alyraffauf/cattery/internal/deployment"
	"github.com/alyraffauf/cattery/internal/failure"
	"github.com/alyraffauf/cattery/internal/pathsafe"
	"github.com/alyraffauf/cattery/internal/repository"
)

// inferContext bundles the read-only inputs of ownership inference so Infer
// stays under the parameter limit. Targets are canonical absolute paths
// resolved against the working directory before inference begins, so Infer
// never touches the filesystem.
type inferContext struct {
	identity RepositoryIdentity
	plan     deployment.Plan
	platform deployment.Layer
	targets  []string
}

// targetRef pairs one canonical absolute target with its HOME-relative form.
type targetRef struct {
	absolute string
	relative string
}

// sourceLocation is the inferred scope, layer, and kind of one source entry.
type sourceLocation struct {
	scope deployment.Scope
	layer deployment.Layer
	kind  deployment.FileKind
}

// Infer derives one ItemPlanInput per target in raw command-line order. It is
// pure and read-only: each canonical absolute target is mapped to its owner
// when the plan already manages it, or to the inferred scope, layer, kind, and
// repository-relative source path under Section 2's grammar. ExecutableBits
// stays zero; preflight fills it from the live target mode.
func Infer(context inferContext, request Request) ([]ItemPlanInput, error) {
	items := make([]ItemPlanInput, 0, len(context.targets))
	for _, target := range context.targets {
		item, err := inferTarget(context, request, target)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// inferTarget maps one target to its item plan, preferring an existing plan
// owner and rejecting alias and unrepresentable targets.
func inferTarget(context inferContext, request Request, target string) (ItemPlanInput, error) {
	ref, err := resolveTargetRef(context.identity, target)
	if err != nil {
		return ItemPlanInput{}, err
	}
	if canonical, named := aliasCanonical(context.plan, ref.relative); named {
		return ItemPlanInput{}, failure.New(failure.InvalidInput,
			"add: target "+ref.relative+" is an alias; add "+canonical+" instead", nil)
	}
	if owner, managed := matchManagedFile(context.plan, ref.relative); managed {
		return inferManaged(request, ref, owner)
	}
	return inferUnmanaged(context, request, ref)
}

// inferManaged adopts the existing owner's scope, layer, kind, and source
// location, rejecting explicit options that contradict it.
func inferManaged(request Request, ref targetRef, owner deployment.ManagedFile) (ItemPlanInput, error) {
	if err := contradictsOwner(owner, request); err != nil {
		return ItemPlanInput{}, err
	}
	return ItemPlanInput{
		Scope:                owner.Scope,
		Layer:                owner.Layer,
		Kind:                 owner.Kind,
		TargetAbsolutePath:   ref.absolute,
		TargetRelativePath:   ref.relative,
		SourceRepositoryPath: owner.SourceRepositoryPath,
		SourceAbsolutePath:   owner.SourceAbsolutePath,
	}, nil
}

// inferUnmanaged derives the default location, proves the target is
// representable there, and inverts Section 2's grammar to produce the source.
func inferUnmanaged(context inferContext, request Request, ref targetRef) (ItemPlanInput, error) {
	location, err := inferLocation(context, request)
	if err != nil {
		return ItemPlanInput{}, err
	}
	if err := checkRepresentable(location.scope, location.layer, ref.relative); err != nil {
		return ItemPlanInput{}, err
	}
	source := location.sourcePath(ref.relative)
	if _, err := pathsafe.Segments(source); err != nil {
		return ItemPlanInput{}, failure.New(failure.InvalidInput, "add: derived source path", err)
	}
	return ItemPlanInput{
		Scope:                location.scope,
		Layer:                location.layer,
		Kind:                 location.kind,
		TargetAbsolutePath:   ref.absolute,
		TargetRelativePath:   ref.relative,
		SourceRepositoryPath: source,
		SourceAbsolutePath:   filepath.Join(context.identity.Root, source),
	}, nil
}

// inferLocation selects the scope, layer, and kind for an unmanaged target.
func inferLocation(context inferContext, request Request) (sourceLocation, error) {
	layer, err := inferLayer(context, request)
	if err != nil {
		return sourceLocation{}, err
	}
	return sourceLocation{scope: inferScope(request), layer: layer, kind: inferKind(request)}, nil
}

// sourcePath inverts Section 2's grammar into a repository-relative source
// path from the location and the HOME-relative target.
func (location sourceLocation) sourcePath(target string) string {
	var builder strings.Builder
	if !location.scope.IsRoot() {
		builder.WriteString(location.scope.Group)
		builder.WriteString("/")
	}
	if location.layer != deployment.LayerBase {
		builder.WriteString("_")
		builder.WriteString(string(location.layer))
		builder.WriteString("/")
	}
	if location.kind == deployment.FileSecret {
		builder.WriteString("_secrets/")
	}
	builder.WriteString(target)
	return builder.String()
}

// resolveTargetRef strips the canonical home prefix and validates the result.
func resolveTargetRef(identity RepositoryIdentity, target string) (targetRef, error) {
	relative, err := homeRelative(identity, target)
	if err != nil {
		return targetRef{}, err
	}
	return targetRef{absolute: target, relative: relative}, nil
}

// inferScope selects an explicit group or the root default.
func inferScope(request Request) deployment.Scope {
	if request.GroupSet {
		return deployment.NewScope(request.Group)
	}
	return deployment.NewScope("")
}

// inferLayer selects an explicit platform layer that must match the runtime
// platform, or the base default.
func inferLayer(context inferContext, request Request) (deployment.Layer, error) {
	if !request.PlatformSet {
		return deployment.LayerBase, nil
	}
	layer, err := deployment.ParseLayer(request.Platform)
	if err != nil {
		return "", failure.New(failure.InvalidInput, "add: --platform "+request.Platform, err)
	}
	if layer != context.platform {
		return "", failure.New(failure.InvalidInput,
			"add: --platform must equal runtime platform "+string(context.platform), nil)
	}
	return layer, nil
}

// inferKind selects the secret kind only when --secret is present and true.
func inferKind(request Request) deployment.FileKind {
	if request.SecretSet && request.Secret {
		return deployment.FileSecret
	}
	return deployment.FileOrdinary
}

// checkRepresentable enforces Section 2.1: underscore-prefixed targets are
// unrepresentable everywhere; root base additionally rejects metadata names
// and multi-segment non-dot targets that the grammar would route to a group.
func checkRepresentable(scope deployment.Scope, layer deployment.Layer, relative string) error {
	first := strings.SplitN(relative, "/", 2)[0]
	if strings.HasPrefix(first, "_") {
		return failure.New(failure.InvalidInput,
			"add: target "+relative+" begins with an underscore and is unrepresentable", nil)
	}
	if !scope.IsRoot() || layer != deployment.LayerBase {
		return nil
	}
	if repository.ClassifyRoot(first) == repository.ControlMetadata {
		return failure.New(failure.InvalidInput,
			"add: target "+relative+" is a reserved metadata name; pass --group", nil)
	}
	if strings.Contains(relative, "/") && !strings.HasPrefix(first, ".") {
		return failure.New(failure.InvalidInput,
			"add: target "+relative+" is unrepresentable at the root base layer; pass --group", nil)
	}
	return nil
}

// homeRelative strips the canonical home prefix from target and validates the
// remaining slash-relative form.
func homeRelative(identity RepositoryIdentity, target string) (string, error) {
	prefix := identity.Home + "/"
	if !strings.HasPrefix(target, prefix) {
		return "", failure.New(failure.InvalidInput, "add: target is not beneath $HOME", nil)
	}
	relative := strings.TrimPrefix(target, prefix)
	if _, err := pathsafe.Segments(relative); err != nil {
		return "", failure.New(failure.InvalidInput, "add: target "+relative, err)
	}
	return relative, nil
}

// matchManagedFile returns the plan owner whose target matches relative.
func matchManagedFile(plan deployment.Plan, relative string) (deployment.ManagedFile, bool) {
	for _, file := range plan.Files() {
		if file.TargetRelativePath == relative {
			return file, true
		}
	}
	return deployment.ManagedFile{}, false
}

// aliasCanonical reports whether relative is a configured alias and returns
// the canonical target the caller should add instead.
func aliasCanonical(plan deployment.Plan, relative string) (string, bool) {
	for _, alias := range plan.Aliases() {
		if alias.AliasRelativePath == relative {
			return alias.CanonicalTargetRelativePath, true
		}
	}
	return "", false
}

// contradictsOwner rejects explicit options that disagree with the owner.
func contradictsOwner(owner deployment.ManagedFile, request Request) error {
	if request.GroupSet && request.Group != owner.Scope.Group {
		return failure.New(failure.InvalidInput,
			"add: --group conflicts with the existing owner of "+owner.TargetRelativePath, nil)
	}
	if request.PlatformSet && explicitLayer(request.Platform) != owner.Layer {
		return failure.New(failure.InvalidInput,
			"add: --platform conflicts with the existing owner of "+owner.TargetRelativePath, nil)
	}
	if request.SecretSet && request.Secret != (owner.Kind == deployment.FileSecret) {
		return failure.New(failure.InvalidInput,
			"add: --secret conflicts with the existing owner of "+owner.TargetRelativePath, nil)
	}
	return nil
}

// explicitLayer parses the raw platform value, returning the zero layer on
// any error so a contradiction is reported rather than a parse failure.
func explicitLayer(value string) deployment.Layer {
	layer, err := deployment.ParseLayer(value)
	if err != nil {
		return ""
	}
	return layer
}
