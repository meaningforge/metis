// Package policy implements the application-owned data policy boundary.
// A validated snapshot is NOT a
// permission to execute: semantic binding and relation-local enforcement must
// succeed before any compiled artifact is released or executed.
package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/meaningforge/metis/app/auth"
	"github.com/meaningforge/metis/query"
)

// Bounds apply to the entire request/decision, not independently per source.
// Bounds reject work; they never truncate security constraints.
const (
	MaxSources       = 128
	MaxFields        = 4096
	MaxPredicates    = 256
	MaxValues        = 4096
	MaxValueBytes    = 65536
	MaxIdentityBytes = 1024
	MaxScopes        = 256
)

// DatasetRef and FieldRef contain exact, resolved Ossie names, not SQL names,
// aliases, or display labels. Structured identities avoid delimiter ambiguity.
// Their existence and complete dependency closure remain resolver obligations.
type DatasetRef struct{ Model, Dataset string }
type FieldRef struct {
	Dataset DatasetRef
	Field   string
}
type Source struct {
	Dataset        DatasetRef
	RequiredFields []FieldRef
}
type Request struct {
	Principal auth.Principal
	ProjectID string
	Sources   []Source
}

type Effect string

const (
	Unrestricted Effect = "unrestricted"
	Constrained  Effect = "constrained"
	Denied       Effect = "denied"
)

// Predicate values use a closed scalar domain: string, bool, int64, float64.
// Integer values do not pass through float64. Dates/timestamps are strings and
// are checked against the selected field type by the semantic binder. Values
// are homogeneous, flat, finite and bounded; null operators have zero values.
type Predicate struct {
	Field    FieldRef
	Operator query.FilterOperator
	Values   []any
}
type SourceConstraint struct {
	Dataset       DatasetRef
	RowPredicates []Predicate
	DeniedFields  []FieldRef
}
type Decision struct {
	Effect  Effect
	Sources []SourceConstraint
}

// DataAccessPolicy is invoked once per top-level operation, after project and
// asset authorization. Implementations must honor context cancellation and
// must not mutate input or output concurrently with evaluation/copying.
// Action, transport, dialect, SQL and executable callbacks are not policy data.
type DataAccessPolicy interface {
	Evaluate(context.Context, Request) (Decision, error)
}

// NoRestrictionDataAccessPolicy is an explicit compatibility choice, never a
// fallback for a missing or malfunctioning adapter.
type NoRestrictionDataAccessPolicy struct{}

func (NoRestrictionDataAccessPolicy) Evaluate(context.Context, Request) (Decision, error) {
	return Decision{Effect: Unrestricted}, nil
}

// Errors deliberately contain no adapter error, policy data, or identity.
// The semantic service maps these to stable, redacted public error codes.
var (
	ErrDenied      = errors.New("data access denied")
	ErrUnavailable = errors.New("data access policy unavailable")
	ErrInvalid     = errors.New("invalid data access policy")
)

// Snapshot owns private copies. Zero snapshots are invalid. Accessors also
// return copies: neither adapters nor consumers can mutate an in-flight policy.
type Snapshot struct {
	request  Request
	decision Decision
	valid    bool
}

func (s Snapshot) Effect() Effect                  { return s.decision.Effect }
func (s Snapshot) Constraints() []SourceConstraint { return cloneDecision(s.decision).Sources }

// ScopeID is private equality evidence for scan fusion, not telemetry or
// user-facing explanation. It covers the complete typed decision and workload.
func (s Snapshot) ScopeID() string {
	if !s.valid {
		return ""
	}
	payload, err := json.Marshal(struct {
		Request  Request
		Decision Decision
	}{s.request, s.decision})
	if err != nil {
		return ""
	}
	hash := sha256.New()
	_, _ = hash.Write(payload)
	for _, source := range s.decision.Sources {
		for _, predicate := range source.RowPredicates {
			for _, value := range predicate.Values {
				typ, _ := json.Marshal(reflect.TypeOf(value).String())
				_, _ = hash.Write(typ)
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// Matches checks the exact authenticated workload without reevaluating policy.
// Request order is significant; callers must reuse the same canonical workload.
// Semantic generation identity must additionally be retained by its owner.
func (s Snapshot) Matches(req Request) bool { return s.valid && reflect.DeepEqual(s.request, req) }

// Evaluate obtains one decision and validates coverage, structure and direct
// deny intersections. It does not resolve fields, prove physical locality or
// inject predicates. Binding and enforcement must use this same snapshot.
func Evaluate(ctx context.Context, policy DataAccessPolicy, req Request) (snapshot Snapshot, err error) {
	if ctx == nil || ctx.Err() != nil || nilPolicy(policy) {
		return Snapshot{}, ErrUnavailable
	}
	if !validRequest(req) {
		return Snapshot{}, ErrInvalid
	}
	// Keep separate copies: an adapter may mutate its own request synchronously.
	owned := cloneRequest(req)
	defer func() {
		if recover() != nil {
			snapshot, err = Snapshot{}, ErrUnavailable
		}
	}()
	decision, adapterErr := policy.Evaluate(ctx, cloneRequest(owned))
	if adapterErr != nil || ctx.Err() != nil {
		return Snapshot{}, ErrUnavailable
	}
	if err := validateDecision(owned, decision); err != nil {
		return Snapshot{}, err
	}
	canonical := cloneDecision(decision)
	// Conjunction and deny sets have no adapter-order meaning. Canonicalize only
	// these sets; preserve scalar order (notably the endpoints of between).
	key := func(value any) string { data, _ := json.Marshal(value); return string(data) }
	for i := range canonical.Sources {
		source := &canonical.Sources[i]
		sort.Slice(source.DeniedFields, func(i, j int) bool { return key(source.DeniedFields[i]) < key(source.DeniedFields[j]) })
		sort.Slice(source.RowPredicates, func(i, j int) bool {
			left, right := source.RowPredicates[i], source.RowPredicates[j]
			lk, rk := key(left), key(right)
			if lk != rk {
				return lk < rk
			}
			if len(left.Values) == 0 {
				return false
			}
			return reflect.TypeOf(left.Values[0]).String() < reflect.TypeOf(right.Values[0]).String()
		})
	}
	sort.Slice(canonical.Sources, func(i, j int) bool { return key(canonical.Sources[i].Dataset) < key(canonical.Sources[j].Dataset) })
	return Snapshot{request: owned, decision: canonical, valid: true}, nil
}

func nilPolicy(policy DataAccessPolicy) bool {
	if policy == nil {
		return true
	}
	v := reflect.ValueOf(policy)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func validName(s string) bool {
	return s != "" && len(s) <= MaxIdentityBytes && strings.TrimSpace(s) == s && !strings.ContainsAny(s, "\x00\r\n")
}
func validDataset(d DatasetRef) bool           { return validName(d.Model) && validName(d.Dataset) }
func validField(f FieldRef, d DatasetRef) bool { return f.Dataset == d && validName(f.Field) }
func validRequest(req Request) bool {
	if !validName(req.ProjectID) || !validName(req.Principal.TenantID) || !validName(req.Principal.SubjectID) ||
		len(req.Principal.APIKeyID) > MaxIdentityBytes || len(req.Principal.Scopes) > MaxScopes ||
		len(req.Sources) == 0 || len(req.Sources) > MaxSources {
		return false
	}
	for _, scope := range req.Principal.Scopes {
		if !validName(scope) {
			return false
		}
	}
	sources := make(map[DatasetRef]bool, len(req.Sources))
	count := 0
	for _, source := range req.Sources {
		if !validDataset(source.Dataset) || sources[source.Dataset] {
			return false
		}
		sources[source.Dataset] = true
		count += len(source.RequiredFields)
		if count > MaxFields {
			return false
		}
		fields := make(map[FieldRef]bool, len(source.RequiredFields))
		for _, field := range source.RequiredFields {
			if !validField(field, source.Dataset) || fields[field] {
				return false
			}
			fields[field] = true
		}
	}
	return true
}

func validateDecision(req Request, decision Decision) error {
	switch decision.Effect {
	case Unrestricted, Denied:
		if len(decision.Sources) != 0 {
			return ErrInvalid
		}
		if decision.Effect == Denied {
			return ErrDenied
		}
		return nil
	case Constrained:
	default:
		return ErrInvalid
	}
	if len(decision.Sources) != len(req.Sources) {
		return ErrInvalid
	}
	required := make(map[DatasetRef]map[FieldRef]bool, len(req.Sources))
	for _, source := range req.Sources {
		fields := make(map[FieldRef]bool, len(source.RequiredFields))
		for _, f := range source.RequiredFields {
			fields[f] = true
		}
		required[source.Dataset] = fields
	}
	seen := make(map[DatasetRef]bool, len(req.Sources))
	predicates, fields, values, bytes := 0, 0, 0, 0
	denied := false
	for _, source := range decision.Sources {
		needed, ok := required[source.Dataset]
		if !ok || seen[source.Dataset] {
			return ErrInvalid
		}
		seen[source.Dataset] = true
		fields += len(source.DeniedFields)
		predicates += len(source.RowPredicates)
		if fields > MaxFields || predicates > MaxPredicates {
			return ErrInvalid
		}
		deny := make(map[FieldRef]bool, len(source.DeniedFields))
		for _, f := range source.DeniedFields {
			if !validField(f, source.Dataset) || deny[f] {
				return ErrInvalid
			}
			deny[f] = true
			denied = denied || needed[f]
		}
		for _, p := range source.RowPredicates {
			if !validField(p.Field, source.Dataset) || deny[p.Field] {
				return ErrInvalid
			}
			values += len(p.Values)
			if values > MaxValues || !validPredicate(p, &bytes) {
				return ErrInvalid
			}
		}
	}
	if denied {
		return ErrDenied
	}
	return nil
}

func validPredicate(p Predicate, bytes *int) bool {
	switch p.Operator {
	case query.FilterIsNull, query.FilterIsNotNull:
		return len(p.Values) == 0
	case query.FilterIN, query.FilterNotIn:
		if len(p.Values) == 0 {
			return false
		}
	case query.FilterBetween:
		if len(p.Values) != 2 {
			return false
		}
	case query.FilterEQ, query.FilterNEQ, query.FilterGT, query.FilterGTE, query.FilterLT, query.FilterLTE:
		if len(p.Values) != 1 {
			return false
		}
	default:
		return false
	}
	var kind reflect.Type
	for _, value := range p.Values {
		switch v := value.(type) {
		case string:
			*bytes += len(v)
		case bool, int64:
			*bytes += 8
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return false
			}
			*bytes += 8
		default:
			return false
		}
		if *bytes > MaxValueBytes {
			return false
		}
		if kind != nil && kind != reflect.TypeOf(value) {
			return false
		}
		kind = reflect.TypeOf(value)
	}
	return true
}

func cloneRequest(req Request) Request {
	req.Principal.Scopes = cloneSlice(req.Principal.Scopes)
	req.Sources = cloneSlice(req.Sources)
	for i := range req.Sources {
		req.Sources[i].RequiredFields = cloneSlice(req.Sources[i].RequiredFields)
	}
	return req
}
func cloneDecision(d Decision) Decision {
	d.Sources = cloneSlice(d.Sources)
	for i := range d.Sources {
		s := &d.Sources[i]
		s.DeniedFields = cloneSlice(s.DeniedFields)
		s.RowPredicates = cloneSlice(s.RowPredicates)
		for j := range s.RowPredicates {
			s.RowPredicates[j].Values = cloneSlice(s.RowPredicates[j].Values)
		}
	}
	return d
}
func cloneSlice[T any](v []T) []T {
	if v == nil {
		return nil
	}
	out := make([]T, len(v))
	copy(out, v)
	return out
}
