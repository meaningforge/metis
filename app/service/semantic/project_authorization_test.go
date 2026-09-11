package semantic

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/query"
	"github.com/meaningforge/metis/serrors"
)

func TestProjectActionVocabularyAndScopeMapping(t *testing.T) {
	want := []ProjectAction{
		ProjectActionActivate, ProjectActionAdmin, ProjectActionAuthor, ProjectActionCompile,
		ProjectActionDiscover, ProjectActionExecute, ProjectActionPublish,
	}
	if got := ProjectActions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectActions() = %v, want %v", got, want)
	}
	wantScopes := map[ProjectAction]string{
		ProjectActionDiscover: auth.ScopeSemanticRead,
		ProjectActionCompile:  auth.ScopeSemanticCompile,
		ProjectActionExecute:  auth.ScopeSemanticExecute,
		ProjectActionAuthor:   auth.ScopeSemanticAuthor,
		ProjectActionPublish:  auth.ScopeSemanticPublish,
		ProjectActionActivate: auth.ScopeSemanticActivate,
		ProjectActionAdmin:    auth.ScopeSemanticAdmin,
	}
	for action, wantScope := range wantScopes {
		if scope, ok := action.RequiredScope(); !ok || scope != wantScope {
			t.Errorf("%s scope = %q, %v; want %q, true", action, scope, ok, wantScope)
		}
	}
}

func TestProjectActionsAreIndependentlyControllableByScope(t *testing.T) {
	authorizer := ScopeProjectAuthorizer{}
	for _, action := range ProjectActions() {
		required, _ := action.RequiredScope()
		for _, grantedAction := range ProjectActions() {
			granted, _ := grantedAction.RequiredScope()
			decision := authorizer.AuthorizeProject(context.Background(), ProjectAuthorizationRequest{
				Principal: &auth.Principal{Scopes: []string{granted}}, ProjectID: "finance", Action: action,
			})
			if got, want := decision.Allowed(), granted == required; got != want {
				t.Errorf("action %s with scope %s allowed = %v, want %v", action, granted, got, want)
			}
		}
		wildcard := authorizer.AuthorizeProject(context.Background(), ProjectAuthorizationRequest{
			Principal: &auth.Principal{Scopes: []string{auth.ScopeAll}}, ProjectID: "finance", Action: action,
		})
		if !wildcard.Allowed() || wildcard.Reason != ProjectAuthorizationReasonAllAccess {
			t.Errorf("wildcard decision for %s = %#v", action, wildcard)
		}
	}
}

func TestAuthorizeProjectPassesStableIdentityAndFailsClosed(t *testing.T) {
	principal := &auth.Principal{TenantID: "tenant-1", SubjectID: "user-7", APIKeyID: "key-2", Scopes: []string{auth.ScopeSemanticRead}}
	ctx := auth.WithPrincipal(context.Background(), principal)
	var request ProjectAuthorizationRequest
	var audit ProjectAuthorizationAudit
	authorizer := ProjectAuthorizerFunc(func(_ context.Context, got ProjectAuthorizationRequest) ProjectAuthorizationDecision {
		request = got
		return ProjectAuthorizationDecision{Effect: "future-effect", Reason: "unbounded-policy-message"}
	})
	observer := ProjectAuthorizationObserverFunc(func(_ context.Context, got ProjectAuthorizationAudit) { audit = got })
	err := AuthorizeProject(ctx, authorizer, observer, " finance ", ProjectActionDiscover)
	var semanticErr *serrors.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectAccessDenied {
		t.Fatalf("authorization error = %v", err)
	}
	if request.Principal != principal || request.ProjectID != "finance" || request.Action != ProjectActionDiscover {
		t.Fatalf("authorization request = %#v", request)
	}
	if audit.ProjectID != "finance" || audit.Action != ProjectActionDiscover || audit.Effect != ProjectAuthorizationDeny || audit.Reason != ProjectAuthorizationReasonInvalidDecision {
		t.Fatalf("audit = %#v", audit)
	}
	if audit.TenantID != "tenant-1" || audit.SubjectID != "user-7" || audit.APIKeyID != "key-2" {
		t.Fatalf("audit principal identity = %#v", audit)
	}
	if len(semanticErr.Details) != 2 || semanticErr.Details["action"] != ProjectActionDiscover || semanticErr.Details["project_id"] != "finance" {
		t.Fatalf("public denial details = %#v", semanticErr.Details)
	}
}

func TestProjectAuthorizationAuditHasOnlyApprovedFields(t *testing.T) {
	typeOfAudit := reflect.TypeOf(ProjectAuthorizationAudit{})
	want := []string{"TenantID", "SubjectID", "APIKeyID", "ProjectID", "Action", "Effect", "Reason"}
	got := make([]string, 0, typeOfAudit.NumField())
	for i := 0; i < typeOfAudit.NumField(); i++ {
		got = append(got, typeOfAudit.Field(i).Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProjectAuthorizationAudit fields = %v, want %v", got, want)
	}
}

func TestAuthorizationObserverCannotChangeDecision(t *testing.T) {
	observer := ProjectAuthorizationObserverFunc(func(context.Context, ProjectAuthorizationAudit) { panic("sink failed") })
	if err := AuthorizeProject(context.Background(), AllAccessProjectAuthorizer{}, observer, "finance", ProjectActionAdmin); err != nil {
		t.Fatalf("observer changed allow decision: %v", err)
	}
}

func TestDiscoveryAndCompileDenyBeforeSemanticAccess(t *testing.T) {
	deny := ProjectAuthorizerFunc(func(context.Context, ProjectAuthorizationRequest) ProjectAuthorizationDecision {
		return ProjectAuthorizationDecision{Effect: ProjectAuthorizationDeny, Reason: ProjectAuthorizationReasonPolicyDenied}
	})
	resolver := fixedProjectResolver{project: "finance"}
	discovery := NewDiscoveryService(nil).WithProjectResolver(resolver).WithProjectAuthorizer(deny)
	_, discoveryErr := discovery.GetModel(context.Background(), GetModelRequest{Project: "finance", Model: "secret-model"})
	if errorCode(discoveryErr) != serrors.ErrProjectAccessDenied {
		t.Fatalf("discovery denial = %v", discoveryErr)
	}
	compile := NewCompileService(nil, nil, nil).WithProjectResolver(resolver).WithProjectAuthorizer(deny)
	_, compileErr := compile.Compile(context.Background(), CompileRequest{Query: query.SemanticQuery{Project: "finance"}})
	if errorCode(compileErr) != serrors.ErrProjectAccessDenied {
		t.Fatalf("compile denial = %v", compileErr)
	}
	validation, validationErr := compile.Validate(context.Background(), CompileRequest{Query: query.SemanticQuery{Project: "finance"}})
	if validation != nil || errorCode(validationErr) != serrors.ErrProjectAccessDenied {
		t.Fatalf("validation = %#v, error = %v", validation, validationErr)
	}
}

func TestDirectServiceConstructorsFailClosedWithoutPrincipal(t *testing.T) {
	_, discoveryErr := NewDiscoveryService(nil).GetModel(context.Background(), GetModelRequest{Project: "finance", Model: "secret-model"})
	if errorCode(discoveryErr) != serrors.ErrProjectAccessDenied {
		t.Fatalf("default discovery authorization = %v", discoveryErr)
	}
	_, compileErr := NewCompileService(nil, nil, nil).Compile(context.Background(), CompileRequest{Query: query.SemanticQuery{Project: "finance"}})
	if errorCode(compileErr) != serrors.ErrProjectAccessDenied {
		t.Fatalf("default compile authorization = %v", compileErr)
	}
}

type fixedProjectResolver struct{ project string }

func (r fixedProjectResolver) ResolveProject(string) (string, error) { return r.project, nil }
