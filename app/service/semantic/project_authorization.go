package semantic

import (
	"context"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/serrors"
)

// ProjectAction is the closed public policy vocabulary for project-scoped
// application operations. Adding an action is a policy-contract change.
type ProjectAction string

const (
	ProjectActionDiscover ProjectAction = "discover"
	ProjectActionCompile  ProjectAction = "compile"
	ProjectActionExecute  ProjectAction = "execute"
	ProjectActionAuthor   ProjectAction = "author"
	ProjectActionPublish  ProjectAction = "publish"
	ProjectActionActivate ProjectAction = "activate"
	ProjectActionAdmin    ProjectAction = "admin"
)

var projectActionScopes = map[ProjectAction]string{
	ProjectActionDiscover: auth.ScopeSemanticRead,
	ProjectActionCompile:  auth.ScopeSemanticCompile,
	ProjectActionExecute:  auth.ScopeSemanticExecute,
	ProjectActionAuthor:   auth.ScopeSemanticAuthor,
	ProjectActionPublish:  auth.ScopeSemanticPublish,
	ProjectActionActivate: auth.ScopeSemanticActivate,
	ProjectActionAdmin:    auth.ScopeSemanticAdmin,
}

// ProjectActions returns the complete action vocabulary in deterministic order.
func ProjectActions() []ProjectAction {
	actions := make([]ProjectAction, 0, len(projectActionScopes))
	for action := range projectActionScopes {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i] < actions[j] })
	return actions
}

// RequiredScope returns the default API-key scope mapped to this action.
func (a ProjectAction) RequiredScope() (string, bool) {
	scope, ok := projectActionScopes[a]
	return scope, ok
}

type ProjectAuthorizationEffect string

const (
	ProjectAuthorizationAllow ProjectAuthorizationEffect = "allow"
	ProjectAuthorizationDeny  ProjectAuthorizationEffect = "deny"
)

// ProjectAuthorizationReason is bounded policy evidence. It is suitable for
// logs and metric labels; arbitrary policy-engine messages never cross the
// application boundary.
type ProjectAuthorizationReason string

const (
	ProjectAuthorizationReasonAllAccess         ProjectAuthorizationReason = "all_access"
	ProjectAuthorizationReasonScopeGranted      ProjectAuthorizationReason = "scope_granted"
	ProjectAuthorizationReasonScopeMissing      ProjectAuthorizationReason = "scope_missing"
	ProjectAuthorizationReasonPrincipalMissing  ProjectAuthorizationReason = "principal_missing"
	ProjectAuthorizationReasonPolicyDenied      ProjectAuthorizationReason = "policy_denied"
	ProjectAuthorizationReasonPolicyUnavailable ProjectAuthorizationReason = "policy_unavailable"
	ProjectAuthorizationReasonInvalidAction     ProjectAuthorizationReason = "invalid_action"
	ProjectAuthorizationReasonInvalidProject    ProjectAuthorizationReason = "invalid_project"
	ProjectAuthorizationReasonInvalidDecision   ProjectAuthorizationReason = "invalid_decision"
)

// ProjectAuthorizationRequest is the complete input to a project policy. It
// contains authenticated identity and stable project/action identities only;
// semantic request payloads and credentials do not belong here.
type ProjectAuthorizationRequest struct {
	Principal *auth.Principal
	ProjectID string
	Action    ProjectAction
}

// ProjectAuthorizationDecision is deliberately smaller than a policy-engine
// response. Unrecognized effects fail closed and reasons remain bounded.
type ProjectAuthorizationDecision struct {
	Effect ProjectAuthorizationEffect
	Reason ProjectAuthorizationReason
}

func (d ProjectAuthorizationDecision) Allowed() bool {
	return d.Effect == ProjectAuthorizationAllow
}

// ProjectAuthorizer is the sole transport-neutral project/action policy
// boundary. REST, MCP, CLI-backed administration, and future Web surfaces use
// this same contract.
type ProjectAuthorizer interface {
	AuthorizeProject(context.Context, ProjectAuthorizationRequest) ProjectAuthorizationDecision
}

type ProjectAuthorizerFunc func(context.Context, ProjectAuthorizationRequest) ProjectAuthorizationDecision

func (f ProjectAuthorizerFunc) AuthorizeProject(ctx context.Context, req ProjectAuthorizationRequest) ProjectAuthorizationDecision {
	if f == nil {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonPolicyUnavailable}
	}
	return f(ctx, req)
}

// ScopeProjectAuthorizer maps the closed action vocabulary to the Principal's
// API scopes. It is the default server adapter used with the static API-key
// verifier and remains replaceable by a project-aware SaaS policy store.
type ScopeProjectAuthorizer struct{}

func (ScopeProjectAuthorizer) AuthorizeProject(_ context.Context, req ProjectAuthorizationRequest) ProjectAuthorizationDecision {
	if strings.TrimSpace(req.ProjectID) == "" {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonInvalidProject}
	}
	required, ok := req.Action.RequiredScope()
	if !ok {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonInvalidAction}
	}
	if req.Principal == nil {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonPrincipalMissing}
	}
	if !req.Principal.HasScope(required) {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonScopeMissing}
	}
	reason := ProjectAuthorizationReasonScopeGranted
	if req.Principal.HasScope(auth.ScopeAll) {
		reason = ProjectAuthorizationReasonAllAccess
	}
	return ProjectAuthorizationDecision{Effect: ProjectAuthorizationAllow, Reason: reason}
}

// AllAccessProjectAuthorizer is the trusted local/offline adapter. Production
// servers install ScopeProjectAuthorizer or a deployment policy instead.
type AllAccessProjectAuthorizer struct{}

func (AllAccessProjectAuthorizer) AuthorizeProject(_ context.Context, req ProjectAuthorizationRequest) ProjectAuthorizationDecision {
	if strings.TrimSpace(req.ProjectID) == "" {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonInvalidProject}
	}
	if _, ok := req.Action.RequiredScope(); !ok {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonInvalidAction}
	}
	return ProjectAuthorizationDecision{Effect: ProjectAuthorizationAllow, Reason: ProjectAuthorizationReasonAllAccess}
}

type ProjectAuthorizationAudit struct {
	TenantID  string
	SubjectID string
	APIKeyID  string
	ProjectID string
	Action    ProjectAction
	Effect    ProjectAuthorizationEffect
	Reason    ProjectAuthorizationReason
}

type ProjectAuthorizationObserver interface {
	ObserveProjectAuthorization(context.Context, ProjectAuthorizationAudit)
}

type ProjectAuthorizationObserverFunc func(context.Context, ProjectAuthorizationAudit)

func (f ProjectAuthorizationObserverFunc) ObserveProjectAuthorization(ctx context.Context, audit ProjectAuthorizationAudit) {
	if f != nil {
		f(ctx, audit)
	}
}

func authorizeProject(ctx context.Context, authorizer ProjectAuthorizer, observer ProjectAuthorizationObserver, projectID string, action ProjectAction) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	principal, _ := auth.PrincipalFromContext(ctx)
	req := ProjectAuthorizationRequest{Principal: principal, ProjectID: strings.TrimSpace(projectID), Action: action}
	decision := ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonPolicyUnavailable}
	if authorizer != nil {
		decision = authorizer.AuthorizeProject(ctx, req)
	}
	if decision.Effect != ProjectAuthorizationAllow && decision.Effect != ProjectAuthorizationDeny {
		decision = ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonInvalidDecision}
	}
	if !validProjectAuthorizationReason(decision.Reason) {
		decision = ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonInvalidDecision}
	}
	audit := ProjectAuthorizationAudit{ProjectID: req.ProjectID, Action: action, Effect: decision.Effect, Reason: decision.Reason}
	if principal != nil {
		audit.TenantID, audit.SubjectID, audit.APIKeyID = principal.TenantID, principal.SubjectID, principal.APIKeyID
	}
	observeProjectAuthorization(ctx, observer, audit)
	if decision.Allowed() {
		return nil
	}
	return &serrors.Error{
		Code: serrors.ErrProjectAccessDenied, Message: "project action is not authorized",
		Details: map[string]any{"project_id": req.ProjectID, "action": action},
	}
}

func validProjectAuthorizationReason(reason ProjectAuthorizationReason) bool {
	switch reason {
	case ProjectAuthorizationReasonAllAccess, ProjectAuthorizationReasonScopeGranted, ProjectAuthorizationReasonScopeMissing,
		ProjectAuthorizationReasonPrincipalMissing, ProjectAuthorizationReasonPolicyDenied, ProjectAuthorizationReasonPolicyUnavailable,
		ProjectAuthorizationReasonInvalidAction, ProjectAuthorizationReasonInvalidProject, ProjectAuthorizationReasonInvalidDecision:
		return true
	default:
		return false
	}
}

// AuthorizeProject evaluates one project/action decision and returns the stable
// PROJECT_ACCESS_DENIED contract for every denial.
func AuthorizeProject(ctx context.Context, authorizer ProjectAuthorizer, observer ProjectAuthorizationObserver, projectID string, action ProjectAction) error {
	return authorizeProject(ctx, authorizer, observer, projectID, action)
}

func canAuthorizeProject(ctx context.Context, authorizer ProjectAuthorizer, observer ProjectAuthorizationObserver, projectID string, action ProjectAction) bool {
	return authorizeProject(ctx, authorizer, observer, projectID, action) == nil
}

func observeProjectAuthorization(ctx context.Context, observer ProjectAuthorizationObserver, audit ProjectAuthorizationAudit) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer.ObserveProjectAuthorization(ctx, audit)
}
