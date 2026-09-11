package attribution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
	"github.com/meaningforge/metis/cmd/s2sbench/bench/scenarios"
)

const evidenceTolerance = 1e-8

type Judgement struct {
	Verdict                s2sbench.Verdict `json:"verdict"`
	Reason                 string           `json:"reason,omitempty"`
	ReconciliationFailures int              `json:"reconciliation_failures"`
	AnswerFingerprint      string           `json:"answer_fingerprint"`
}

func JudgeEvidence(scenarioName string, evidence EvidenceBundle) (Judgement, error) {
	if _, err := ScenarioByName(scenarioName); err != nil {
		return Judgement{}, err
	}
	fingerprint, err := FingerprintEvidence(evidence)
	if err != nil {
		return Judgement{}, err
	}
	failures, err := ReconciliationFailures(evidence)
	if err != nil {
		return Judgement{}, err
	}
	judgement := Judgement{Verdict: s2sbench.VerdictCorrect, ReconciliationFailures: failures, AnswerFingerprint: fingerprint}
	if failures > 0 {
		judgement.Verdict = s2sbench.VerdictWrong
		judgement.Reason = fmt.Sprintf("attribution evidence has %d reconciliation failure(s)", failures)
		return judgement, nil
	}
	if err := validateGroundTruth(scenarioName, evidence); err != nil {
		judgement.Verdict = s2sbench.VerdictWrong
		judgement.Reason = err.Error()
	}
	return judgement, nil
}

func FingerprintEvidence(evidence EvidenceBundle) (string, error) {
	encoded, err := json.Marshal(evidence)
	if err != nil {
		return "", fmt.Errorf("encode attribution evidence: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ReconciliationFailures(evidence EvidenceBundle) (int, error) {
	scenario, err := ScenarioByName(evidence.Scenario)
	if err != nil {
		return 0, err
	}
	if len(evidence.Dimensions) != len(scenario.DimensionRefs) {
		return 0, fmt.Errorf("attribution evidence has %d dimensions, want %d", len(evidence.Dimensions), len(scenario.DimensionRefs))
	}
	failures := 0
	for _, dimension := range evidence.Dimensions {
		index, err := columnIndex(dimension.Result)
		if err != nil {
			return 0, fmt.Errorf("dimension %q: %w", dimension.Dimension, err)
		}
		switch scenario.Decomposition {
		case DecompositionAdditive:
			for _, required := range []string{"delta", "total_delta", "contribution_pct"} {
				if _, ok := index[required]; !ok {
					return 0, fmt.Errorf("dimension %q is missing %q", dimension.Dimension, required)
				}
			}
			var sum float64
			var total *float64
			for _, row := range dimension.Result.Rows {
				delta, valueErr := numeric(row[index["delta"]])
				if valueErr != nil {
					return 0, fmt.Errorf("dimension %q delta: %w", dimension.Dimension, valueErr)
				}
				sum += delta
				rowTotal, valueErr := numeric(row[index["total_delta"]])
				if valueErr != nil {
					return 0, fmt.Errorf("dimension %q total delta: %w", dimension.Dimension, valueErr)
				}
				if total == nil {
					total = &rowTotal
				} else if !approx(*total, rowTotal) {
					failures++
				}
				if approx(rowTotal, 0) && !row[index["contribution_pct"]].Null {
					failures++
				}
			}
			if total == nil || !approx(sum, *total) {
				failures++
			}
		case DecompositionRatio:
			for _, required := range []string{"segment_defined", "segment_effect", "ratio_delta", "decomposed_delta", "reconciliation_residual", "attribution_defined"} {
				if _, ok := index[required]; !ok {
					return 0, fmt.Errorf("dimension %q is missing %q", dimension.Dimension, required)
				}
			}
			var sum float64
			var ratioDelta, decomposed, residual optionalNumberState
			defined := true
			for _, row := range dimension.Result.Rows {
				rowDefined, valueErr := truth(row[index["attribution_defined"]])
				if valueErr != nil {
					return 0, valueErr
				}
				defined = defined && rowDefined
				segmentDefined, valueErr := truth(row[index["segment_defined"]])
				if valueErr != nil {
					return 0, valueErr
				}
				effect := row[index["segment_effect"]]
				if !segmentDefined && !effect.Null {
					failures++
				}
				if segmentDefined && !effect.Null {
					value, numericErr := numeric(effect)
					if numericErr != nil {
						return 0, numericErr
					}
					sum += value
				}
				failures, err = ratioDelta.observe(row[index["ratio_delta"]], failures)
				if err != nil {
					return 0, err
				}
				failures, err = decomposed.observe(row[index["decomposed_delta"]], failures)
				if err != nil {
					return 0, err
				}
				failures, err = residual.observe(row[index["reconciliation_residual"]], failures)
				if err != nil {
					return 0, err
				}
			}
			if defined {
				if !ratioDelta.defined() || !decomposed.defined() || !residual.defined() || !approx(sum, decomposed.value) || !approx(ratioDelta.value, decomposed.value) || !approx(residual.value, 0) {
					failures++
				}
			} else if !ratioDelta.nullOnly() || !decomposed.nullOnly() || !residual.nullOnly() {
				failures++
			}
		default:
			return 0, fmt.Errorf("unsupported attribution decomposition %q", scenario.Decomposition)
		}
	}
	return failures, nil
}

func validateGroundTruth(scenarioName string, evidence EvidenceBundle) error {
	if evidence.Scenario != scenarioName {
		return fmt.Errorf("evidence scenario is %q, want %q", evidence.Scenario, scenarioName)
	}
	scenario, _ := ScenarioByName(scenarioName)
	wantDimensions := append([]string(nil), scenario.DimensionRefs...)
	sort.Strings(wantDimensions)
	if len(evidence.Dimensions) != len(wantDimensions) {
		return fmt.Errorf("evidence dimensions = %d, want %d", len(evidence.Dimensions), len(wantDimensions))
	}
	for i, dimension := range evidence.Dimensions {
		if dimension.Dimension != wantDimensions[i] {
			return fmt.Errorf("evidence dimension %d is %q, want %q", i, dimension.Dimension, wantDimensions[i])
		}
	}
	switch scenarioName {
	case "additive_population_union":
		return validateAdditive(evidence, map[string]map[string]additiveExpected{
			"channel": {
				"store": {Baseline: 50, Current: 80, Delta: 30, Total: 50, Contribution: numberPointer(60)},
				"web":   {Baseline: 100, Current: 120, Delta: 20, Total: 50, Contribution: numberPointer(40)},
			},
			"region": {
				"east":  {Baseline: 0, Current: 80, Delta: 80, Total: 50, Contribution: numberPointer(160)},
				"south": {Baseline: 50, Current: 0, Delta: -50, Total: 50, Contribution: numberPointer(-100)},
				"north": {Baseline: 100, Current: 120, Delta: 20, Total: 50, Contribution: numberPointer(40)},
			},
		})
	case "additive_offsetting_zero_total":
		return validateAdditive(evidence, map[string]map[string]additiveExpected{
			"segment": {
				"A": {Baseline: 100, Current: 120, Delta: 20, Total: 0},
				"B": {Baseline: 50, Current: 30, Delta: -20, Total: 0},
			},
		})
	case "ratio_mix_rate_entry_exit":
		return validateRatioDefined(evidence, map[string]float64{"A": 0.0466666666666667, "B": -0.0333333333333333, "C": 0.08}, 0.0933333333333333)
	case "ratio_undefined_denominator":
		return validateRatioUndefined(evidence)
	default:
		return fmt.Errorf("unknown attribution ground truth %q", scenarioName)
	}
}

type additiveExpected struct {
	Baseline     float64
	Current      float64
	Delta        float64
	Total        float64
	Contribution *float64
}

func validateAdditive(evidence EvidenceBundle, expected map[string]map[string]additiveExpected) error {
	for _, dimension := range evidence.Dimensions {
		want := expected[dimension.Dimension]
		rows, index, err := rowsByDimension(dimension)
		if err != nil {
			return err
		}
		if err := requireColumns(index, "baseline_value", "current_value", "delta", "total_delta", "contribution_pct"); err != nil {
			return fmt.Errorf("dimension %q: %w", dimension.Dimension, err)
		}
		if len(rows) != len(want) {
			return fmt.Errorf("dimension %q returned %d rows, want %d", dimension.Dimension, len(rows), len(want))
		}
		for value, expectedRow := range want {
			row, ok := rows[value]
			if !ok {
				return fmt.Errorf("dimension %q is missing value %q", dimension.Dimension, value)
			}
			for name, expectedValue := range map[string]float64{"baseline_value": expectedRow.Baseline, "current_value": expectedRow.Current, "delta": expectedRow.Delta, "total_delta": expectedRow.Total} {
				actual, valueErr := numeric(row[index[name]])
				if valueErr != nil || !approx(actual, expectedValue) {
					return fmt.Errorf("dimension %q value %q %s does not match ground truth", dimension.Dimension, value, name)
				}
			}
			contribution := row[index["contribution_pct"]]
			if expectedRow.Contribution == nil {
				if !contribution.Null {
					return fmt.Errorf("dimension %q value %q contribution must be null", dimension.Dimension, value)
				}
			} else if actual, valueErr := numeric(contribution); valueErr != nil || !approx(actual, *expectedRow.Contribution) {
				return fmt.Errorf("dimension %q value %q contribution does not match ground truth", dimension.Dimension, value)
			}
		}
	}
	return nil
}

func validateRatioDefined(evidence EvidenceBundle, effects map[string]float64, delta float64) error {
	dimension := evidence.Dimensions[0]
	rows, index, err := rowsByDimension(dimension)
	if err != nil {
		return err
	}
	if err := requireColumns(index, "segment_effect", "ratio_delta", "attribution_defined"); err != nil {
		return err
	}
	if len(rows) != len(effects) {
		return fmt.Errorf("ratio evidence returned %d rows, want %d", len(rows), len(effects))
	}
	for value, effect := range effects {
		row, ok := rows[value]
		if !ok {
			return fmt.Errorf("ratio evidence is missing segment %q", value)
		}
		actualEffect, valueErr := numeric(row[index["segment_effect"]])
		actualDelta, deltaErr := numeric(row[index["ratio_delta"]])
		defined, definedErr := truth(row[index["attribution_defined"]])
		if valueErr != nil || deltaErr != nil || definedErr != nil || !defined || !approx(actualEffect, effect) || !approx(actualDelta, delta) {
			return fmt.Errorf("ratio segment %q does not match ground truth", value)
		}
	}
	return nil
}

func validateRatioUndefined(evidence EvidenceBundle) error {
	dimension := evidence.Dimensions[0]
	rows, index, err := rowsByDimension(dimension)
	if err != nil {
		return err
	}
	if err := requireColumns(index, "segment_defined", "attribution_defined", "segment_effect", "current_ratio", "ratio_delta", "decomposed_delta", "reconciliation_residual"); err != nil {
		return err
	}
	row, ok := rows["D"]
	if !ok {
		return fmt.Errorf("undefined ratio evidence is missing segment D")
	}
	segmentDefined, segmentErr := truth(row[index["segment_defined"]])
	attributionDefined, attributionErr := truth(row[index["attribution_defined"]])
	if segmentErr != nil || attributionErr != nil || segmentDefined || attributionDefined || !row[index["segment_effect"]].Null || !row[index["current_ratio"]].Null || !row[index["ratio_delta"]].Null || !row[index["decomposed_delta"]].Null || !row[index["reconciliation_residual"]].Null {
		return fmt.Errorf("undefined ratio evidence does not preserve null/defined state")
	}
	return nil
}

func rowsByDimension(dimension DimensionEvidence) (map[string]scenarios.ResultRow, map[string]int, error) {
	index, err := columnIndex(dimension.Result)
	if err != nil {
		return nil, nil, err
	}
	dimensionIndex, ok := index[dimension.Dimension]
	if !ok {
		return nil, nil, fmt.Errorf("dimension result is missing %q", dimension.Dimension)
	}
	rows := make(map[string]scenarios.ResultRow, len(dimension.Result.Rows))
	for _, row := range dimension.Result.Rows {
		value := row[dimensionIndex].Canonical
		if _, exists := rows[value]; exists {
			return nil, nil, fmt.Errorf("dimension %q contains duplicate value %q", dimension.Dimension, value)
		}
		rows[value] = row
	}
	return rows, index, nil
}

func columnIndex(result scenarios.ResultSet) (map[string]int, error) {
	index := make(map[string]int, len(result.Columns))
	for i, column := range result.Columns {
		if strings.TrimSpace(column.Name) == "" {
			return nil, fmt.Errorf("result column %d has no name", i)
		}
		if _, exists := index[column.Name]; exists {
			return nil, fmt.Errorf("result contains duplicate column %q", column.Name)
		}
		index[column.Name] = i
	}
	for i, row := range result.Rows {
		if len(row) != len(result.Columns) {
			return nil, fmt.Errorf("result row %d has %d values, want %d", i, len(row), len(result.Columns))
		}
	}
	return index, nil
}

type optionalNumberState struct {
	seen  bool
	null  bool
	value float64
}

func (state *optionalNumberState) observe(value scenarios.ResultValue, failures int) (int, error) {
	if value.Null {
		if state.seen && !state.null {
			failures++
		}
		state.seen = true
		state.null = true
		return failures, nil
	}
	parsed, err := numeric(value)
	if err != nil {
		return failures, err
	}
	if state.seen && (state.null || !approx(state.value, parsed)) {
		failures++
	}
	state.seen = true
	state.null = false
	state.value = parsed
	return failures, nil
}

func (state optionalNumberState) defined() bool {
	return state.seen && !state.null
}

func (state optionalNumberState) nullOnly() bool {
	return state.seen && state.null
}

func numeric(value scenarios.ResultValue) (float64, error) {
	if value.Null {
		return 0, fmt.Errorf("numeric evidence is null")
	}
	rational, ok := new(big.Rat).SetString(value.Canonical)
	if !ok {
		return 0, fmt.Errorf("numeric evidence %q is invalid", value.Canonical)
	}
	parsed, _ := rational.Float64()
	return parsed, nil
}

func truth(value scenarios.ResultValue) (bool, error) {
	if value.Null {
		return false, fmt.Errorf("boolean evidence is null")
	}
	switch value.Canonical {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("boolean evidence %q is invalid", value.Canonical)
	}
}

func approx(left, right float64) bool {
	return math.Abs(left-right) <= evidenceTolerance
}

func numberPointer(value float64) *float64 {
	return &value
}

func requireColumns(index map[string]int, names ...string) error {
	for _, name := range names {
		if _, ok := index[name]; !ok {
			return fmt.Errorf("result is missing column %q", name)
		}
	}
	return nil
}
