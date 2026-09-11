package auth

// Principal is the stable authenticated identity exposed to Metis request handling.
// Authentication identifies the tenant, subject, and credential. Project access is
// an authorization decision and therefore intentionally does not live here.
type Principal struct {
	TenantID  string   `json:"tenant_id"`
	SubjectID string   `json:"subject_id"`
	APIKeyID  string   `json:"api_key_id"`
	Scopes    []string `json:"scopes"`
}

const (
	ScopeAll              = "*"
	ScopeSemanticRead     = "semantic:read"
	ScopeSemanticCompile  = "semantic:compile"
	ScopeSemanticExecute  = "semantic:execute"
	ScopeSemanticAuthor   = "semantic:author"
	ScopeSemanticPublish  = "semantic:publish"
	ScopeSemanticActivate = "semantic:activate"
	ScopeSemanticAdmin    = "semantic:admin"
)

// HasScope reports whether the principal holds one required scope. The
// wildcard is deliberately the only implication between scopes; compile does
// not imply execute.
func (p Principal) HasScope(required string) bool {
	for _, scope := range p.Scopes {
		if scope == ScopeAll || scope == required {
			return true
		}
	}
	return false
}
