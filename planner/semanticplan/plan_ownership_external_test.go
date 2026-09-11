package semanticplan_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/extension"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/planner/semanticplan"
	"github.com/meaningforge/metis/query"
)

func TestSemanticPlanCloneCarriesEveryField(t *testing.T) {
	dropped := map[string]string{
		"semanticplan.SemanticPlan.OptimizationTrace": "cloneSemanticPlan clears it because a trace describes how one plan was reached",
	}

	original := filledSemanticPlan(t)
	clone := semanticplan.ClonePlan(&original)

	var undeclared []string
	for _, reported := range findUnclonedFields(reflect.ValueOf(original), reflect.ValueOf(*clone), "semanticplan.SemanticPlan") {
		path, _, _ := strings.Cut(reported, " ")
		if _, ok := dropped[path]; ok || prefixDeclared(dropped, path) {
			continue
		}
		undeclared = append(undeclared, reported)
	}
	if len(undeclared) != 0 {
		sort.Strings(undeclared)
		t.Fatalf("cloneSemanticPlan does not carry every field:\n  %s", strings.Join(undeclared, "\n  "))
	}
}

func TestSemanticPlanCloneSharesNoStorage(t *testing.T) {
	readOnlyPointers := map[reflect.Type]string{
		reflect.TypeOf((*semanticplan.RelationPolicy)(nil)):         "relation constraints have private immutable storage and copying accessors",
		reflect.TypeOf((*expression.ResolvedAnalysis)(nil)):         "resolved analysis is immutable planner input",
		reflect.TypeOf((*expression.ExtensionEvidenceSet)(nil)):     "evidence sets are replaced, not mutated",
		reflect.TypeOf((*query.TimeGrain)(nil)):                     "query grain values are read-only",
		reflect.TypeOf((*semanticplan.CustomCalendarGrouping)(nil)): "custom calendar declarations are read-only",
	}

	original := filledSemanticPlan(t)
	clone := semanticplan.ClonePlan(&original)

	shared := findSharedStorage(reflect.ValueOf(original), reflect.ValueOf(*clone), "semanticplan.SemanticPlan", false)
	exercised := map[reflect.Type]bool{}
	var undeclared []string
	for _, alias := range shared {
		if alias.insideOssieDeclaration {
			continue
		}
		if _, ok := readOnlyPointers[alias.typ]; ok {
			exercised[alias.typ] = true
			continue
		}
		if strings.HasSuffix(alias.path, ".Filter.Value") {
			continue
		}
		undeclared = append(undeclared, fmt.Sprintf("%s (%s)", alias.path, alias.typ))
	}
	if len(undeclared) != 0 {
		sort.Strings(undeclared)
		t.Fatalf("cloneSemanticPlan shares storage with its input at:\n  %s", strings.Join(undeclared, "\n  "))
	}

	for typ, reason := range readOnlyPointers {
		if !exercised[typ] {
			t.Errorf("%s is declared shareable (%s) but nothing shares it any more", typ, reason)
		}
	}
}

func prefixDeclared(declared map[string]string, path string) bool {
	for prefix := range declared {
		if strings.HasPrefix(path, prefix+".") {
			return true
		}
	}
	return false
}

func filledSemanticPlan(t *testing.T) semanticplan.SemanticPlan {
	t.Helper()
	var plan semanticplan.SemanticPlan
	blind := fillValue(reflect.ValueOf(&plan).Elem(), "seed", "semanticplan.SemanticPlan")
	if len(blind) != 0 {
		sort.Strings(blind)
		t.Fatalf("ownership test cannot seed:\n  %s", strings.Join(blind, "\n  "))
	}
	return plan
}

const ossiePackage = "github.com/meaningforge/metis/ossie"

type aliasPoint struct {
	path                   string
	typ                    reflect.Type
	insideOssieDeclaration bool
}

type ownershipContract struct {
	uncloned func(original, clone reflect.Value, path string) []string
	shared   func(original, clone reflect.Value, path string) []aliasPoint
}

var contracts = map[reflect.Type]ownershipContract{
	reflect.TypeOf((*semanticplan.RelationPolicy)(nil)): {
		uncloned: func(original, clone reflect.Value, path string) []string {
			if !reflect.DeepEqual(original.Interface(), clone.Interface()) {
				return []string{path + " (relation policy changed)"}
			}
			return nil
		},
		shared: func(original, clone reflect.Value, path string) []aliasPoint {
			if original.Pointer() != 0 && original.Pointer() == clone.Pointer() {
				return []aliasPoint{{path: path, typ: original.Type()}}
			}
			return nil
		},
	},
	reflect.TypeOf(expression.ResolvedExpression{}): {
		uncloned: func(original, clone reflect.Value, path string) []string {
			from := original.Interface().(expression.ResolvedExpression)
			to := clone.Interface().(expression.ResolvedExpression)
			var out []string
			if from.SourceDialect != to.SourceDialect {
				out = append(out, fmt.Sprintf("%s.SourceDialect (%q became %q)", path, from.SourceDialect, to.SourceDialect))
			}
			if from.Source != to.Source {
				out = append(out, fmt.Sprintf("%s.Source (%q became %q)", path, from.Source, to.Source))
			}
			if from.Analysis != nil && to.Analysis == nil {
				out = append(out, path+".Analysis (dropped)")
			}
			if want, got := from.ExtensionEvidenceValues(), to.ExtensionEvidenceValues(); !reflect.DeepEqual(want, got) {
				out = append(out, fmt.Sprintf("%s.ExtensionEvidence (%v became %v)", path, want, got))
			}
			return out
		},
		shared: func(original, clone reflect.Value, path string) []aliasPoint {
			from := original.Interface().(expression.ResolvedExpression)
			to := clone.Interface().(expression.ResolvedExpression)
			var out []aliasPoint
			if from.Analysis != nil && from.Analysis == to.Analysis {
				out = append(out, aliasPoint{path: path + ".Analysis", typ: reflect.TypeOf(from.Analysis)})
			}
			if from.ExtensionEvidence != nil && from.ExtensionEvidence == to.ExtensionEvidence {
				out = append(out, aliasPoint{path: path + ".ExtensionEvidence", typ: reflect.TypeOf(from.ExtensionEvidence)})
			}
			return out
		},
	},
}

var seeders = map[reflect.Type]func(seed string) reflect.Value{
	reflect.TypeOf((*semanticplan.RelationPolicy)(nil)): func(seed string) reflect.Value {
		policy, err := semanticplan.NewRelationPolicy(seed, seed, []semanticplan.RelationPredicate{{Field: seed, Column: seed, Datatype: ossie.DataTypeString, Operator: query.FilterEQ, Values: []any{seed}}})
		if err != nil {
			panic(err)
		}
		return reflect.ValueOf(policy)
	},
	reflect.TypeOf(expression.ResolvedExpression{}): func(seed string) reflect.Value {
		resolved := expression.NewResolvedExpression("DUCKDB", seed).
			WithAnalysis(
				expression.BoundExpression{Bindings: map[expression.Reference]expression.BoundSymbol{}},
				expression.TypedExpression{Deterministic: true},
			).
			WithExtensionEvidence(extension.MetricScaleEvidence{
				Identity: extension.Identity{},
				Metric:   seed,
				Version:  "v1",
				Factor:   2,
			})
		return reflect.ValueOf(resolved)
	},
	reflect.TypeOf((*extension.Evidence)(nil)).Elem(): func(seed string) reflect.Value {
		return reflect.ValueOf(extension.Evidence(extension.MetricScaleEvidence{
			Identity: extension.Identity{},
			Metric:   seed,
			Version:  "v1",
			Factor:   3,
		}))
	},
	reflect.TypeOf((*any)(nil)).Elem(): func(seed string) reflect.Value {
		return reflect.ValueOf(any(map[string]string{"ai_context": seed}))
	},
}

func findUnclonedFields(original, clone reflect.Value, path string) []string {
	if !original.IsValid() || !clone.IsValid() {
		return nil
	}
	if contract, ok := contracts[original.Type()]; ok {
		return contract.uncloned(original, clone, path)
	}
	switch original.Kind() {
	case reflect.Pointer, reflect.Interface:
		if original.IsNil() {
			return nil
		}
		if clone.IsNil() {
			return []string{path + " (dropped)"}
		}
		return findUnclonedFields(original.Elem(), clone.Elem(), path)
	case reflect.Struct:
		var out []string
		for i := 0; i < original.NumField(); i++ {
			field := original.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			out = append(out, findUnclonedFields(original.Field(i), clone.Field(i), path+"."+field.Name)...)
		}
		return out
	case reflect.Slice:
		if original.IsNil() {
			return nil
		}
		if clone.IsNil() {
			return []string{path + " (dropped)"}
		}
		if original.Len() != clone.Len() {
			return []string{fmt.Sprintf("%s (length %d became %d)", path, original.Len(), clone.Len())}
		}
		var out []string
		for i := 0; i < original.Len(); i++ {
			out = append(out, findUnclonedFields(original.Index(i), clone.Index(i), fmt.Sprintf("%s[%d]", path, i))...)
		}
		return out
	case reflect.Map:
		if original.IsNil() {
			return nil
		}
		if clone.IsNil() {
			return []string{path + " (dropped)"}
		}
		if original.Len() != clone.Len() {
			return []string{fmt.Sprintf("%s (length %d became %d)", path, original.Len(), clone.Len())}
		}
		var out []string
		for _, key := range original.MapKeys() {
			cloned := clone.MapIndex(key)
			if !cloned.IsValid() {
				out = append(out, fmt.Sprintf("%s[%v] (key dropped)", path, key.Interface()))
				continue
			}
			out = append(out, findUnclonedFields(original.MapIndex(key), cloned, fmt.Sprintf("%s[%v]", path, key.Interface()))...)
		}
		return out
	default:
		if !reflect.DeepEqual(original.Interface(), clone.Interface()) {
			return []string{fmt.Sprintf("%s (%v became %v)", path, original.Interface(), clone.Interface())}
		}
		return nil
	}
}

func findSharedStorage(original, clone reflect.Value, path string, inOssie bool) []aliasPoint {
	if !original.IsValid() || !clone.IsValid() {
		return nil
	}
	if contract, ok := contracts[original.Type()]; ok {
		return contract.shared(original, clone, path)
	}
	switch original.Kind() {
	case reflect.Pointer:
		if original.IsNil() || clone.IsNil() {
			return nil
		}
		if original.Type().Elem().Size() == 0 {
			return findSharedStorage(original.Elem(), clone.Elem(), path, inOssie)
		}
		if original.Pointer() == clone.Pointer() {
			return []aliasPoint{{path: path, typ: original.Type(), insideOssieDeclaration: inOssie || declaredInOssie(original.Type())}}
		}
		return findSharedStorage(original.Elem(), clone.Elem(), path, inOssie)
	case reflect.Interface:
		if original.IsNil() || clone.IsNil() {
			return nil
		}
		return findSharedStorage(original.Elem(), clone.Elem(), path, inOssie)
	case reflect.Struct:
		inOssie = inOssie || declaredInOssie(original.Type())
		var out []aliasPoint
		for i := 0; i < original.NumField(); i++ {
			field := original.Type().Field(i)
			if field.PkgPath != "" {
				continue
			}
			out = append(out, findSharedStorage(original.Field(i), clone.Field(i), path+"."+field.Name, inOssie)...)
		}
		return out
	case reflect.Slice:
		if original.IsNil() || clone.IsNil() || original.Len() == 0 || clone.Len() == 0 {
			return nil
		}
		if original.Type().Elem().Size() != 0 && original.Pointer() == clone.Pointer() {
			return []aliasPoint{{path: path, typ: original.Type(), insideOssieDeclaration: inOssie || declaredInOssie(original.Type().Elem())}}
		}
		var out []aliasPoint
		for i := 0; i < original.Len() && i < clone.Len(); i++ {
			out = append(out, findSharedStorage(original.Index(i), clone.Index(i), fmt.Sprintf("%s[%d]", path, i), inOssie)...)
		}
		return out
	case reflect.Map:
		if original.IsNil() || clone.IsNil() {
			return nil
		}
		if original.Pointer() == clone.Pointer() {
			return []aliasPoint{{path: path, typ: original.Type(), insideOssieDeclaration: inOssie}}
		}
		var out []aliasPoint
		for _, key := range original.MapKeys() {
			cloned := clone.MapIndex(key)
			if !cloned.IsValid() {
				continue
			}
			out = append(out, findSharedStorage(original.MapIndex(key), cloned, fmt.Sprintf("%s[%v]", path, key.Interface()), inOssie)...)
		}
		return out
	default:
		return nil
	}
}

func declaredInOssie(typ reflect.Type) bool {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	return typ.PkgPath() == ossiePackage
}

func hasUnexportedStorage(typ reflect.Type) bool {
	return hasUnexportedStorageSeen(typ, map[reflect.Type]bool{})
}

func hasUnexportedStorageSeen(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if seen[typ] {
		return false
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return hasUnexportedStorageSeen(typ.Elem(), seen)
	case reflect.Map:
		return hasUnexportedStorageSeen(typ.Key(), seen) || hasUnexportedStorageSeen(typ.Elem(), seen)
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.PkgPath != "" || hasUnexportedStorageSeen(field.Type, seen) {
				return true
			}
		}
	}
	return false
}

func fillValue(value reflect.Value, seed, path string) []string {
	if !value.CanSet() {
		return nil
	}
	if construct, ok := seeders[value.Type()]; ok {
		value.Set(construct(seed))
		if _, ok := contracts[value.Type()]; !ok && hasUnexportedStorage(value.Type()) {
			return []string{fmt.Sprintf("%s (%s is seeded but has no ownership contract)", path, value.Type())}
		}
		return nil
	}
	switch value.Kind() {
	case reflect.String:
		value.SetString(seed)
	case reflect.Bool:
		value.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(7)
	case reflect.Float32, reflect.Float64:
		value.SetFloat(7)
	case reflect.Pointer:
		value.Set(reflect.New(value.Type().Elem()))
		return fillValue(value.Elem(), seed, path)
	case reflect.Struct:
		var blind []string
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if field.PkgPath != "" {
				blind = append(blind, fmt.Sprintf("%s (%s has unexported storage)", path, value.Type()))
				continue
			}
			blind = append(blind, fillValue(value.Field(i), seed+"."+field.Name, path+"."+field.Name)...)
		}
		return blind
	case reflect.Interface:
		return []string{fmt.Sprintf("%s (interface %s has no concrete value to seed)", path, value.Type())}
	case reflect.Slice:
		element := reflect.New(value.Type().Elem()).Elem()
		blind := fillValue(element, seed+"[0]", path+"[0]")
		value.Set(reflect.Append(value, element))
		return blind
	case reflect.Map:
		key := reflect.New(value.Type().Key()).Elem()
		blind := fillValue(key, seed+".key", path+".key")
		element := reflect.New(value.Type().Elem()).Elem()
		blind = append(blind, fillValue(element, seed+".value", path+".value")...)
		value.Set(reflect.MakeMap(value.Type()))
		value.SetMapIndex(key, element)
		return blind
	}
	return nil
}
