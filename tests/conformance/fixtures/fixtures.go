package fixtures

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

// ID identifies a canonical semantic-model fixture without coupling shared
// scenarios to its physical file path or encoded representation.
type ID string

const (
	Commerce                   ID = "commerce"
	CommerceAdversarial        ID = "commerce_adversarial"
	DistinctValues             ID = "distinct_values"
	DefinitionFilters          ID = "definition_filters"
	Conversion                 ID = "conversion"
	OffsetToGrain              ID = "offset_to_grain"
	OffsetToGrainDense         ID = "offset_to_grain_dense"
	CustomOffsetToGrainDense   ID = "custom_offset_to_grain_dense"
	CustomCalendarOffset       ID = "custom_calendar_offset"
	CustomCalendarDense        ID = "custom_calendar_dense"
	CustomCalendarRolling      ID = "custom_calendar_rolling"
	CustomCalendarGrainToDate  ID = "custom_calendar_grain_to_date"
	CustomCalendarSemiAdditive ID = "custom_calendar_semi_additive"
	SemiAdditiveTieBreak       ID = "semi_additive_tie_break"
	SemiAdditiveNullSkip       ID = "semi_additive_null_skip"
	SemiAdditiveQueriedWindow  ID = "semi_additive_queried_window"
	SemiAdditiveWindowGrouping ID = "semi_additive_window_grouping"

	ConformanceProject           = "conformance"
	CommerceModel                = "commerce"
	DistinctValuesModel          = "distinct_values"
	DefinitionFiltersModel       = "definition_filters"
	ConversionModel              = "conversion"
	OffsetToGrainModel           = "offset_to_grain"
	OffsetToGrainDenseModel      = "offset_to_grain_dense"
	CustomOffsetToGrainModel     = "custom_offset_to_grain_dense"
	CustomCalendarOffsetModel    = "fiscal_offset"
	CustomCalendarDenseModel     = "fiscal_dense"
	CustomCalendarRollingModel   = "fiscal_rolling"
	CustomCalendarGTDModel       = "fiscal_gtd"
	CustomCalendarInventoryModel = "fiscal_inventory"
	SemiAdditiveEdgesModel       = "semi_additive_edges"
)

// Definition binds a stable fixture ID to its semantic namespace and embedded
// Ossie document.
type Definition struct {
	ID       ID
	Project  string
	Model    string
	Document []byte
}

// CommerceModelYAML is the canonical semantic model fixture shared by compiler
// conformance and real-engine execution backends.
//
//go:embed commerce.ossie.yaml
var CommerceModelYAML []byte

// DistinctValuesModelYAML isolates query-intent behavior that does not require
// the larger commerce semantic world.
//
//go:embed distinct_values.ossie.yaml
var DistinctValuesModelYAML []byte

// DefinitionFiltersModelYAML covers intrinsic metric predicates before and
// after aggregation.
//
//go:embed definition_filters.ossie.yaml
var DefinitionFiltersModelYAML []byte

// ConversionModelYAML defines deterministic base-event to conversion-event
// assignment and calculation semantics.
//
//go:embed conversion.ossie.yaml
var ConversionModelYAML []byte

// OffsetToGrainModelYAML defines built-in boundary-aligned metrics.
//
//go:embed offset_to_grain.ossie.yaml
var OffsetToGrainModelYAML []byte

// OffsetToGrainDenseModelYAML adds a built-in time spine.
//
//go:embed offset_to_grain_dense.ossie.yaml
var OffsetToGrainDenseModelYAML []byte

// CustomOffsetToGrainDenseModelYAML defines dense custom boundary alignment.
//
//go:embed custom_offset_to_grain_dense.ossie.yaml
var CustomOffsetToGrainDenseModelYAML []byte

// CustomCalendarOffsetModelYAML defines ordinal-based custom time offsets.
//
//go:embed custom_calendar_offset.ossie.yaml
var CustomCalendarOffsetModelYAML []byte

// CustomCalendarDenseModelYAML defines custom-period zero filling.
//
//go:embed custom_calendar_dense.ossie.yaml
var CustomCalendarDenseModelYAML []byte

// CustomCalendarRollingModelYAML defines bounded custom-period windows.
//
//go:embed custom_calendar_rolling.ossie.yaml
var CustomCalendarRollingModelYAML []byte

// CustomCalendarGrainToDateModelYAML defines custom hierarchy resets.
//
//go:embed custom_calendar_grain_to_date.ossie.yaml
var CustomCalendarGrainToDateModelYAML []byte

// CustomCalendarSemiAdditiveModelYAML defines ordered custom-period snapshots.
//
//go:embed custom_calendar_semi_additive.ossie.yaml
var CustomCalendarSemiAdditiveModelYAML []byte

// SemiAdditiveEdgesModelYAML defines advanced ordered-selection semantics.
// Multiple fixture IDs reuse this semantic contract with purpose-built
// physical datasets so each result expectation remains focused.
//
//go:embed semi_additive_edges.ossie.yaml
var SemiAdditiveEdgesModelYAML []byte

// AmbiguousPathsModelYAML is a dedicated fixture for validating that multiple
// equivalent relationship paths fail explicitly instead of being chosen by
// traversal or declaration order.
//
//go:embed ambiguous_paths.ossie.yaml
var AmbiguousPathsModelYAML []byte

var definitions = map[ID]Definition{
	Commerce: {
		ID:       Commerce,
		Project:  ConformanceProject,
		Model:    CommerceModel,
		Document: CommerceModelYAML,
	},
	CommerceAdversarial: {
		ID:       CommerceAdversarial,
		Project:  ConformanceProject,
		Model:    CommerceModel,
		Document: CommerceModelYAML,
	},
	DistinctValues: {
		ID:       DistinctValues,
		Project:  ConformanceProject,
		Model:    DistinctValuesModel,
		Document: DistinctValuesModelYAML,
	},
	DefinitionFilters: {
		ID:       DefinitionFilters,
		Project:  ConformanceProject,
		Model:    DefinitionFiltersModel,
		Document: DefinitionFiltersModelYAML,
	},
	Conversion: {
		ID:       Conversion,
		Project:  ConformanceProject,
		Model:    ConversionModel,
		Document: ConversionModelYAML,
	},
	OffsetToGrain: {
		ID:       OffsetToGrain,
		Project:  ConformanceProject,
		Model:    OffsetToGrainModel,
		Document: OffsetToGrainModelYAML,
	},
	OffsetToGrainDense: {
		ID:       OffsetToGrainDense,
		Project:  ConformanceProject,
		Model:    OffsetToGrainDenseModel,
		Document: OffsetToGrainDenseModelYAML,
	},
	CustomOffsetToGrainDense: {
		ID:       CustomOffsetToGrainDense,
		Project:  ConformanceProject,
		Model:    CustomOffsetToGrainModel,
		Document: CustomOffsetToGrainDenseModelYAML,
	},
	CustomCalendarOffset: {
		ID:       CustomCalendarOffset,
		Project:  ConformanceProject,
		Model:    CustomCalendarOffsetModel,
		Document: CustomCalendarOffsetModelYAML,
	},
	CustomCalendarDense: {
		ID:       CustomCalendarDense,
		Project:  ConformanceProject,
		Model:    CustomCalendarDenseModel,
		Document: CustomCalendarDenseModelYAML,
	},
	CustomCalendarRolling: {
		ID:       CustomCalendarRolling,
		Project:  ConformanceProject,
		Model:    CustomCalendarRollingModel,
		Document: CustomCalendarRollingModelYAML,
	},
	CustomCalendarGrainToDate: {
		ID:       CustomCalendarGrainToDate,
		Project:  ConformanceProject,
		Model:    CustomCalendarGTDModel,
		Document: CustomCalendarGrainToDateModelYAML,
	},
	CustomCalendarSemiAdditive: {
		ID:       CustomCalendarSemiAdditive,
		Project:  ConformanceProject,
		Model:    CustomCalendarInventoryModel,
		Document: CustomCalendarSemiAdditiveModelYAML,
	},
	SemiAdditiveTieBreak: {
		ID:       SemiAdditiveTieBreak,
		Project:  ConformanceProject,
		Model:    SemiAdditiveEdgesModel,
		Document: SemiAdditiveEdgesModelYAML,
	},
	SemiAdditiveNullSkip: {
		ID:       SemiAdditiveNullSkip,
		Project:  ConformanceProject,
		Model:    SemiAdditiveEdgesModel,
		Document: SemiAdditiveEdgesModelYAML,
	},
	SemiAdditiveQueriedWindow: {
		ID:       SemiAdditiveQueriedWindow,
		Project:  ConformanceProject,
		Model:    SemiAdditiveEdgesModel,
		Document: SemiAdditiveEdgesModelYAML,
	},
	SemiAdditiveWindowGrouping: {
		ID:       SemiAdditiveWindowGrouping,
		Project:  ConformanceProject,
		Model:    SemiAdditiveEdgesModel,
		Document: SemiAdditiveEdgesModelYAML,
	},
}

// Lookup returns the canonical definition for id.
func Lookup(id ID) (Definition, bool) {
	definition, ok := definitions[id]
	return definition, ok
}

// CanonicalSemanticModels returns one deterministic definition for every
// unique semantic model in the conformance project. Multiple physical fixture
// IDs may intentionally share one semantic contract; contradictory documents
// for the same project/model identity fail closed.
func CanonicalSemanticModels() ([]Definition, error) {
	all := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		all = append(all, definition)
	}
	return canonicalSemanticModels(all)
}

func canonicalSemanticModels(definitions []Definition) ([]Definition, error) {
	byIdentity := make(map[string]Definition)
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Project) == "" || strings.TrimSpace(definition.Model) == "" || len(definition.Document) == 0 {
			return nil, fmt.Errorf("fixture %q has an incomplete semantic model identity/document", definition.ID)
		}
		identity := definition.Project + "\x00" + definition.Model
		if existing, ok := byIdentity[identity]; ok {
			if !bytes.Equal(existing.Document, definition.Document) {
				return nil, fmt.Errorf("fixtures %q and %q define different documents for semantic model %s/%s", existing.ID, definition.ID, definition.Project, definition.Model)
			}
			continue
		}
		definition.Document = append([]byte(nil), definition.Document...)
		byIdentity[identity] = definition
	}
	models := make([]Definition, 0, len(byIdentity))
	for _, definition := range byIdentity {
		models = append(models, definition)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Project != models[j].Project {
			return models[i].Project < models[j].Project
		}
		return models[i].Model < models[j].Model
	})
	return models, nil
}
