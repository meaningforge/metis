package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/meaningforge/metis/execution"
	"github.com/meaningforge/metis/expression"
	"github.com/meaningforge/metis/ossie"
	"github.com/meaningforge/metis/serrors"
)

const (
	DiagnosticEquivalentMetricDefinition   DiagnosticCode = "MODEL_QUALITY_EQUIVALENT_METRIC_DEFINITION"
	DiagnosticAliasConflict                DiagnosticCode = "MODEL_QUALITY_ALIAS_CONFLICT"
	DiagnosticDiscoveryIdentityCollision   DiagnosticCode = "MODEL_QUALITY_DISCOVERY_IDENTITY_COLLISION"
	DiagnosticDisconnectedDataset          DiagnosticCode = "MODEL_QUALITY_DISCONNECTED_DATASET"
	DiagnosticUnreachableDimension         DiagnosticCode = "MODEL_QUALITY_UNREACHABLE_DIMENSION"
	DiagnosticAmbiguousRelationshipPath    DiagnosticCode = "MODEL_QUALITY_AMBIGUOUS_RELATIONSHIP_PATH"
	DiagnosticMetricSourceUnresolved       DiagnosticCode = "MODEL_QUALITY_METRIC_SOURCE_UNRESOLVED"
	DiagnosticMetricTypeMismatch           DiagnosticCode = "MODEL_QUALITY_METRIC_TYPE_MISMATCH"
	DiagnosticMetricAggregationMismatch    DiagnosticCode = "MODEL_QUALITY_METRIC_AGGREGATION_MISMATCH"
	DiagnosticMetricExpressionInconsistent DiagnosticCode = "MODEL_QUALITY_METRIC_EXPRESSION_EVIDENCE_INCONSISTENT"
	DiagnosticDeprecatedWithoutReplacement DiagnosticCode = "MODEL_QUALITY_DEPRECATED_ASSET_WITHOUT_REPLACEMENT"
	DiagnosticCertifiedWithoutOwner        DiagnosticCode = "MODEL_QUALITY_CERTIFIED_ASSET_WITHOUT_OWNER"
)

const (
	ReasonExactDefinition               DiagnosticReason = "exact_definition"
	ReasonGovernedAlias                 DiagnosticReason = "governed_alias"
	ReasonCanonicalOrAliasIdentity      DiagnosticReason = "canonical_or_alias_identity"
	ReasonMetricSourceResolution        DiagnosticReason = "metric_source_resolution"
	ReasonMultipleShortestPaths         DiagnosticReason = "multiple_shortest_paths"
	ReasonNoMetricSourcePath            DiagnosticReason = "no_metric_source_path"
	ReasonDisconnectedDataset           DiagnosticReason = "disconnected_dataset"
	ReasonDeclaredExpressionType        DiagnosticReason = "declared_expression_type"
	ReasonDialectTypeOrAggregation      DiagnosticReason = "dialect_type_or_aggregation"
	ReasonRowDependentScalar            DiagnosticReason = "row_dependent_scalar"
	ReasonStructuredExpectedType        DiagnosticReason = "structured_expected_type"
	ReasonStructuredExpectedAggregation DiagnosticReason = "structured_expected_aggregation"
	ReasonDeprecatedWithoutReplacement  DiagnosticReason = "deprecated_without_replacement"
	ReasonCertifiedWithoutOwner         DiagnosticReason = "certified_without_owner"
)

type qualityRule struct {
	code     DiagnosticCode
	severity DiagnosticSeverity
	evaluate func(*ProjectSource) ([]qualityFinding, error)
}

type qualityFinding struct {
	asset         string
	relatedAssets []string
	message       string
	evidence      DiagnosticEvidence
}

type QualityRegistry struct {
	rules []qualityRule
}

func NewQualityRegistry() *QualityRegistry {
	return &QualityRegistry{rules: []qualityRule{
		{code: DiagnosticEquivalentMetricDefinition, severity: SeverityWarning, evaluate: equivalentMetricFindings},
		{code: DiagnosticAliasConflict, severity: SeverityWarning, evaluate: aliasConflictFindings},
		{code: DiagnosticDiscoveryIdentityCollision, severity: SeverityWarning, evaluate: discoveryIdentityFindings},
		{code: DiagnosticDisconnectedDataset, severity: SeverityWarning, evaluate: graphQualityFindings(DiagnosticDisconnectedDataset)},
		{code: DiagnosticUnreachableDimension, severity: SeverityWarning, evaluate: graphQualityFindings(DiagnosticUnreachableDimension)},
		{code: DiagnosticAmbiguousRelationshipPath, severity: SeverityWarning, evaluate: graphQualityFindings(DiagnosticAmbiguousRelationshipPath)},
		{code: DiagnosticMetricSourceUnresolved, severity: SeverityWarning, evaluate: graphQualityFindings(DiagnosticMetricSourceUnresolved)},
		{code: DiagnosticMetricTypeMismatch, severity: SeverityWarning, evaluate: metricEvidenceFindings(DiagnosticMetricTypeMismatch)},
		{code: DiagnosticMetricAggregationMismatch, severity: SeverityWarning, evaluate: metricEvidenceFindings(DiagnosticMetricAggregationMismatch)},
		{code: DiagnosticMetricExpressionInconsistent, severity: SeverityWarning, evaluate: metricEvidenceFindings(DiagnosticMetricExpressionInconsistent)},
		{code: DiagnosticDeprecatedWithoutReplacement, severity: SeverityWarning, evaluate: governanceFindings(DiagnosticDeprecatedWithoutReplacement)},
		{code: DiagnosticCertifiedWithoutOwner, severity: SeverityWarning, evaluate: governanceFindings(DiagnosticCertifiedWithoutOwner)},
	}}
}

func EvaluateQuality(candidate *ProjectSource, policy execution.QualityPolicyConfig) (QualityReport, error) {
	return NewQualityRegistry().Evaluate(candidate, policy)
}

// ValidateQualityPolicy checks policy names without loading a candidate or
// evaluating evidence. Managed configuration uses the same rule registry.
func ValidateQualityPolicy(policy execution.QualityPolicyConfig) error {
	known := map[string]bool{}
	for _, rule := range NewQualityRegistry().rules {
		known[string(rule.code)] = true
	}
	for code, severity := range policy.Severities {
		if !known[code] || severityRank(DiagnosticSeverity(severity)) < 0 {
			return fmt.Errorf("invalid quality policy")
		}
	}
	if policy.PublicationThreshold != "" && severityRank(DiagnosticSeverity(policy.PublicationThreshold)) < 0 {
		return fmt.Errorf("invalid quality threshold")
	}
	return nil
}

func (r *QualityRegistry) Evaluate(candidate *ProjectSource, policy execution.QualityPolicyConfig) (QualityReport, error) {
	if candidate == nil || candidate.Document == nil || candidate.Manifest == nil {
		return QualityReport{}, fmt.Errorf("validated semantic candidate is required")
	}
	if r == nil || len(r.rules) == 0 {
		return QualityReport{}, fmt.Errorf("model quality registry is empty")
	}
	severityByCode := make(map[DiagnosticCode]DiagnosticSeverity, len(r.rules))
	for _, rule := range r.rules {
		if rule.code == "" || severityRank(rule.severity) < 0 || rule.evaluate == nil {
			return QualityReport{}, fmt.Errorf("model quality registry contains an invalid rule")
		}
		if _, exists := severityByCode[rule.code]; exists {
			return QualityReport{}, fmt.Errorf("duplicate model quality rule %q", rule.code)
		}
		severityByCode[rule.code] = rule.severity
	}
	for _, code := range sortedKeys(policy.Severities) {
		severity := string(policy.Severities[code])
		typedCode := DiagnosticCode(code)
		if _, exists := severityByCode[typedCode]; !exists {
			return QualityReport{}, fmt.Errorf("unknown model quality diagnostic code %q", code)
		}
		severity = strings.ToLower(strings.TrimSpace(severity))
		typedSeverity := DiagnosticSeverity(severity)
		if severityRank(typedSeverity) < 0 {
			return QualityReport{}, fmt.Errorf("invalid severity %q for model quality diagnostic %q", severity, code)
		}
		severityByCode[typedCode] = typedSeverity
	}
	threshold := DiagnosticSeverity(strings.ToLower(strings.TrimSpace(string(policy.PublicationThreshold))))
	if threshold == "" {
		threshold = SeverityError
	}
	thresholdRank := severityRank(threshold)
	if thresholdRank < 0 {
		return QualityReport{}, fmt.Errorf("invalid model quality publication threshold %q", threshold)
	}

	report := QualityReport{
		SchemaVersion: QualitySchemaVersion, ProjectID: candidate.Bundle.ProjectID,
		ContentDigest: candidate.Bundle.ContentDigest, PublicationThreshold: threshold,
		Publishable: true, Diagnostics: []Diagnostic{},
	}
	for _, rule := range r.rules {
		findings, err := rule.evaluate(candidate)
		if err != nil {
			return QualityReport{}, fmt.Errorf("evaluate model quality rule %s: %w", rule.code, err)
		}
		for _, finding := range findings {
			if finding.asset == "" || finding.message == "" || finding.evidence.Reason == "" {
				return QualityReport{}, fmt.Errorf("model quality rule %s returned an incomplete finding", rule.code)
			}
			related := append([]string(nil), finding.relatedAssets...)
			sort.Strings(related)
			diagnostic := Diagnostic{
				Code: rule.code, Severity: severityByCode[rule.code], Asset: finding.asset,
				RelatedAssets: related, Location: diagnosticLocation(candidate.assetLocations[finding.asset]),
				Message: finding.message, CallerAction: serrors.CallerActionChangeModel, Evidence: &finding.evidence,
			}
			report.Diagnostics = append(report.Diagnostics, diagnostic)
			if severityRank(diagnostic.Severity) <= thresholdRank {
				report.Publishable = false
			}
		}
	}
	appendOntologyQuality(candidate, &report)
	sortDiagnostics(report.Diagnostics)
	return report, nil
}

func sortDiagnostics(diagnostics []Diagnostic) {
	sort.Slice(diagnostics, func(i, j int) bool {
		left, right := diagnostics[i], diagnostics[j]
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Asset != right.Asset {
			return left.Asset < right.Asset
		}
		if diagnosticLocationKey(left.Location) != diagnosticLocationKey(right.Location) {
			return diagnosticLocationKey(left.Location) < diagnosticLocationKey(right.Location)
		}
		leftBody, _ := json.Marshal(left)
		rightBody, _ := json.Marshal(right)
		return string(leftBody) < string(rightBody)
	})
}

func severityRank(severity DiagnosticSeverity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	case SeverityInfo:
		return 2
	default:
		return -1
	}
}

func equivalentMetricFindings(candidate *ProjectSource) ([]qualityFinding, error) {
	var findings []qualityFinding
	for _, model := range sortedModels(candidate.Document) {
		groups := make(map[string][]string)
		for index := range model.Metrics {
			metric := &model.Metrics[index]
			body, err := json.Marshal(struct {
				Datatype   ossie.DataType          `json:"datatype"`
				Expression ossie.Expression        `json:"expression"`
				Extensions []ossie.CustomExtension `json:"custom_extensions,omitempty"`
			}{Datatype: metric.Datatype, Expression: metric.Expression, Extensions: metric.CustomExtensions})
			if err != nil {
				return nil, err
			}
			groups[string(body)] = append(groups[string(body)], metricRef(model.Name, metric.Name))
		}
		keys := sortedKeys(groups)
		for _, key := range keys {
			refs := groups[key]
			if len(refs) < 2 {
				continue
			}
			sort.Strings(refs)
			findings = append(findings, qualityFinding{
				asset: refs[0], relatedAssets: refs[1:],
				message:  "metrics have exact definition-equivalent authored behavior",
				evidence: DiagnosticEvidence{Reason: ReasonExactDefinition, DefinitionDigest: digestBytes([]byte(key)), References: refs, SourceLocations: sourceLocations(candidate, refs)},
			})
		}
	}
	return findings, nil
}

type discoveryIdentities struct {
	canonical map[string][]string
	aliases   map[string][]string
}

func collectDiscoveryIdentities(candidate *ProjectSource) (discoveryIdentities, error) {
	identities := discoveryIdentities{canonical: map[string][]string{}, aliases: map[string][]string{}}
	for _, model := range sortedModels(candidate.Document) {
		for index := range model.Metrics {
			metric := &model.Metrics[index]
			ref := metricRef(model.Name, metric.Name)
			identities.canonical[normalizeDiscoveryIdentity(metric.Name)] = append(identities.canonical[normalizeDiscoveryIdentity(metric.Name)], ref)
			discovery, ok, err := ossie.AgentDiscoverySpec(metric)
			if err != nil {
				return discoveryIdentities{}, err
			}
			if !ok {
				continue
			}
			for _, alias := range discovery.Aliases {
				key := normalizeDiscoveryIdentity(alias)
				identities.aliases[key] = append(identities.aliases[key], ref)
			}
		}
	}
	return identities, nil
}

func aliasConflictFindings(candidate *ProjectSource) ([]qualityFinding, error) {
	identities, err := collectDiscoveryIdentities(candidate)
	if err != nil {
		return nil, err
	}
	var findings []qualityFinding
	for _, identity := range sortedKeys(identities.aliases) {
		refs := uniqueSorted(identities.aliases[identity])
		if len(refs) < 2 {
			continue
		}
		findings = append(findings, qualityFinding{
			asset: refs[0], relatedAssets: refs[1:], message: "governed metric alias is claimed by multiple canonical metrics",
			evidence: DiagnosticEvidence{Reason: ReasonGovernedAlias, NormalizedIdentity: identity, References: refs, SourceLocations: sourceLocations(candidate, refs)},
		})
	}
	return findings, nil
}

func discoveryIdentityFindings(candidate *ProjectSource) ([]qualityFinding, error) {
	identities, err := collectDiscoveryIdentities(candidate)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(identities.canonical)+len(identities.aliases))
	for key := range identities.canonical {
		keys[key] = struct{}{}
	}
	for key := range identities.aliases {
		keys[key] = struct{}{}
	}
	var findings []qualityFinding
	for _, identity := range sortedKeys(keys) {
		canonical := uniqueSorted(identities.canonical[identity])
		all := uniqueSorted(append(append([]string(nil), canonical...), identities.aliases[identity]...))
		if len(all) < 2 || len(canonical) == 0 {
			continue
		}
		findings = append(findings, qualityFinding{
			asset: all[0], relatedAssets: all[1:], message: "canonical metric names and governed aliases create an ambiguous discovery identity",
			evidence: DiagnosticEvidence{Reason: ReasonCanonicalOrAliasIdentity, NormalizedIdentity: identity, CanonicalReferences: canonical, References: all, SourceLocations: sourceLocations(candidate, all)},
		})
	}
	return findings, nil
}

type graphQuality struct {
	disconnected []qualityFinding
	unreachable  []qualityFinding
	ambiguous    []qualityFinding
	unresolved   []qualityFinding
}

func graphQualityFindings(code DiagnosticCode) func(*ProjectSource) ([]qualityFinding, error) {
	return func(candidate *ProjectSource) ([]qualityFinding, error) {
		quality, err := inspectGraphQuality(candidate)
		if err != nil {
			return nil, err
		}
		switch code {
		case DiagnosticDisconnectedDataset:
			return quality.disconnected, nil
		case DiagnosticUnreachableDimension:
			return quality.unreachable, nil
		case DiagnosticAmbiguousRelationshipPath:
			return quality.ambiguous, nil
		case DiagnosticMetricSourceUnresolved:
			return quality.unresolved, nil
		default:
			return nil, fmt.Errorf("unsupported graph quality code %q", code)
		}
	}
}

func inspectGraphQuality(candidate *ProjectSource) (graphQuality, error) {
	var quality graphQuality
	project, err := candidate.Manifest.Project(candidate.Bundle.ProjectID)
	if err != nil {
		return quality, err
	}
	for _, modelName := range sortedKeys(project.Models) {
		model := project.Models[modelName]
		if model == nil || model.Model == nil {
			continue
		}
		sourceSet := map[string]struct{}{}
		for _, metricName := range sortedKeys(model.Metrics) {
			sources, sourceErr := model.MetricSources(metricName)
			if sourceErr != nil {
				var code serrors.ErrorCode
				var semanticErr *serrors.Error
				if errors.As(sourceErr, &semanticErr) {
					code = semanticErr.Code
				}
				quality.unresolved = append(quality.unresolved, qualityFinding{
					asset: metricRef(modelName, metricName), message: "metric source dataset cannot be resolved deterministically",
					evidence: DiagnosticEvidence{Reason: ReasonMetricSourceResolution, CauseCode: code},
				})
				continue
			}
			for _, source := range sources {
				sourceSet[source.Dataset] = struct{}{}
			}
		}
		sources := sortedKeys(sourceSet)
		for _, datasetName := range sortedKeys(model.Datasets) {
			connected := false
			for _, source := range sources {
				_, pathErr := model.ResolveDatasetPaths(source, []string{datasetName})
				if pathErr == nil {
					connected = true
					continue
				}
				var semanticErr *serrors.Error
				if errors.As(pathErr, &semanticErr) && semanticErr.Code == serrors.ErrAmbiguousRelationshipPath {
					connected = true
					quality.ambiguous = append(quality.ambiguous, qualityFinding{
						asset: datasetRef(modelName, datasetName), message: "dataset has multiple shortest relationship paths from a metric source",
						evidence: DiagnosticEvidence{Reason: ReasonMultipleShortestPaths, SourceDataset: source, TargetDataset: datasetName},
					})
				}
			}
			if connected {
				continue
			}
			quality.disconnected = append(quality.disconnected, qualityFinding{
				asset: datasetRef(modelName, datasetName), message: "dataset is disconnected from every resolved metric source",
				evidence: DiagnosticEvidence{Reason: ReasonNoMetricSourcePath, MetricSourceDatasets: sources},
			})
			dataset := model.Datasets[datasetName]
			if dataset == nil {
				continue
			}
			for index := range dataset.Fields {
				field := &dataset.Fields[index]
				if field.Dimension == nil {
					continue
				}
				quality.unreachable = append(quality.unreachable, qualityFinding{
					asset: dimensionRef(modelName, datasetName, field.Name), message: "dimension is unreachable from every resolved metric source",
					evidence: DiagnosticEvidence{Reason: ReasonDisconnectedDataset, Dataset: datasetName, MetricSourceDatasets: sources},
				})
			}
		}
	}
	return quality, nil
}

type metricEvidenceQuality struct {
	types        []qualityFinding
	aggregation  []qualityFinding
	inconsistent []qualityFinding
}

func metricEvidenceFindings(code DiagnosticCode) func(*ProjectSource) ([]qualityFinding, error) {
	return func(candidate *ProjectSource) ([]qualityFinding, error) {
		quality, err := inspectMetricEvidence(candidate)
		if err != nil {
			return nil, err
		}
		switch code {
		case DiagnosticMetricTypeMismatch:
			return quality.types, nil
		case DiagnosticMetricAggregationMismatch:
			return quality.aggregation, nil
		case DiagnosticMetricExpressionInconsistent:
			return quality.inconsistent, nil
		default:
			return nil, fmt.Errorf("unsupported metric evidence code %q", code)
		}
	}
}

func inspectMetricEvidence(candidate *ProjectSource) (metricEvidenceQuality, error) {
	var quality metricEvidenceQuality
	project, err := candidate.Manifest.Project(candidate.Bundle.ProjectID)
	if err != nil {
		return quality, err
	}
	for _, modelName := range sortedKeys(project.Models) {
		model := project.Models[modelName]
		for _, metricName := range sortedKeys(model.Metrics) {
			metric := model.Metrics[metricName]
			analysis, ok := model.MetricAnalysis(metricName)
			if metric == nil || !ok {
				continue
			}
			ref := metricRef(modelName, metricName)
			types, aggregations := map[string]struct{}{}, map[string]struct{}{}
			var rowDependentScalar bool
			for _, analyzed := range analysis.Expressions {
				types[string(analyzed.Typed.Type)] = struct{}{}
				aggregations[string(analyzed.Typed.Aggregation)] = struct{}{}
				rowDependentScalar = rowDependentScalar || (analyzed.Typed.Aggregation == expression.AggregationScalar && analyzed.Typed.RowDependent)
			}
			typeValues, aggregationValues := sortedKeys(types), sortedKeys(aggregations)
			expectedType := semanticTypeForOssie(metric.Datatype)
			if expectedType != expression.TypeUnknown && containsDifferentConcreteType(typeValues, string(expectedType)) {
				quality.types = append(quality.types, qualityFinding{
					asset: ref, message: "declared metric datatype conflicts with analyzed expression type",
					evidence: DiagnosticEvidence{Reason: ReasonDeclaredExpressionType, DeclaredType: metric.Datatype, ExpressionTypes: semanticTypes(typeValues)},
				})
			}
			if len(uniqueConcrete(typeValues, string(expression.TypeUnknown))) > 1 || len(aggregationValues) > 1 {
				quality.inconsistent = append(quality.inconsistent, qualityFinding{
					asset: ref, message: "metric dialect expressions expose inconsistent typed evidence",
					evidence: DiagnosticEvidence{Reason: ReasonDialectTypeOrAggregation, ExpressionTypes: semanticTypes(typeValues), AggregationStates: aggregationStates(aggregationValues)},
				})
			}
			if rowDependentScalar {
				quality.aggregation = append(quality.aggregation, qualityFinding{
					asset: ref, message: "metric expression is row-dependent but has scalar aggregation evidence",
					evidence: DiagnosticEvidence{Reason: ReasonRowDependentScalar, AggregationStates: aggregationStates(aggregationValues)},
				})
			}
			claims, hasClaims, err := ossie.MetricQualityClaims(metric)
			if err != nil {
				return quality, err
			}
			if !hasClaims {
				continue
			}
			if claims.ExpectedType != "" && claims.ExpectedType != metric.Datatype {
				quality.types = append(quality.types, qualityFinding{
					asset: ref, message: "structured expected_type claim contradicts the metric datatype",
					evidence: DiagnosticEvidence{Reason: ReasonStructuredExpectedType, ExpectedType: claims.ExpectedType, DeclaredType: metric.Datatype},
				})
			}
			if claims.ExpectedAggregation != "" && !containsFold(aggregationValues, string(claims.ExpectedAggregation)) {
				quality.aggregation = append(quality.aggregation, qualityFinding{
					asset: ref, message: "structured expected_aggregation claim contradicts analyzed expression evidence",
					evidence: DiagnosticEvidence{Reason: ReasonStructuredExpectedAggregation, ExpectedAggregation: claims.ExpectedAggregation, AggregationStates: aggregationStates(aggregationValues)},
				})
			}
		}
	}
	return quality, nil
}

func sortedModels(document *ossie.Document) []*ossie.SemanticModel {
	models := make([]*ossie.SemanticModel, 0, len(document.SemanticModel))
	for index := range document.SemanticModel {
		models = append(models, &document.SemanticModel[index])
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models
}

type governedQualityAsset struct {
	ref        string
	extensions []ossie.CustomExtension
}

func governanceFindings(code DiagnosticCode) func(*ProjectSource) ([]qualityFinding, error) {
	return func(candidate *ProjectSource) ([]qualityFinding, error) {
		var findings []qualityFinding
		for _, asset := range governedQualityAssets(candidate.Document) {
			governance, authored, err := ossie.ParseAssetGovernance(asset.extensions)
			if err != nil {
				return nil, err
			}
			if !authored {
				continue
			}
			evidence := DiagnosticEvidence{
				Owner: governance.Owner, Lifecycle: governance.Lifecycle, Certification: governance.Certification,
				DeprecationDate: governance.DeprecationDate, Replacement: governance.Replacement,
			}
			switch code {
			case DiagnosticDeprecatedWithoutReplacement:
				if governance.Lifecycle != ossie.AssetLifecycleDeprecated || governance.Replacement != "" {
					continue
				}
				evidence.Reason = ReasonDeprecatedWithoutReplacement
				findings = append(findings, qualityFinding{asset: asset.ref, message: "deprecated semantic asset has no canonical replacement", evidence: evidence})
			case DiagnosticCertifiedWithoutOwner:
				if governance.Certification != ossie.AssetCertificationCertified || governance.Owner != "" {
					continue
				}
				evidence.Reason = ReasonCertifiedWithoutOwner
				findings = append(findings, qualityFinding{asset: asset.ref, message: "certified semantic asset has no owner", evidence: evidence})
			default:
				return nil, fmt.Errorf("unsupported governance quality code %q", code)
			}
		}
		return findings, nil
	}
}

func governedQualityAssets(document *ossie.Document) []governedQualityAsset {
	var assets []governedQualityAsset
	for _, model := range sortedModels(document) {
		assets = append(assets, governedQualityAsset{ref: "model:" + model.Name, extensions: model.CustomExtensions})
		for datasetIndex := range model.Datasets {
			dataset := &model.Datasets[datasetIndex]
			assets = append(assets, governedQualityAsset{ref: datasetRef(model.Name, dataset.Name), extensions: dataset.CustomExtensions})
			for fieldIndex := range dataset.Fields {
				field := &dataset.Fields[fieldIndex]
				if field.Dimension != nil {
					assets = append(assets, governedQualityAsset{ref: dimensionRef(model.Name, dataset.Name, field.Name), extensions: field.CustomExtensions})
				}
			}
		}
		for metricIndex := range model.Metrics {
			metric := &model.Metrics[metricIndex]
			assets = append(assets, governedQualityAsset{ref: metricRef(model.Name, metric.Name), extensions: metric.CustomExtensions})
		}
		for relationshipIndex := range model.Relationships {
			relationship := &model.Relationships[relationshipIndex]
			assets = append(assets, governedQualityAsset{ref: "relationship:" + model.Name + "." + relationship.Name, extensions: relationship.CustomExtensions})
		}
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ref < assets[j].ref })
	return assets
}

func metricRef(model, metric string) string   { return "metric:" + model + "." + metric }
func datasetRef(model, dataset string) string { return "dataset:" + model + "." + dataset }
func dimensionRef(model, dataset, field string) string {
	return "dimension:" + model + "." + dataset + "." + field
}

func normalizeDiscoveryIdentity(value string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, strings.TrimSpace(value))), " ")
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	return sortedKeys(seen)
}

func sourceLocations(candidate *ProjectSource, refs []string) map[string]DiagnosticLocation {
	locations := make(map[string]DiagnosticLocation, len(refs))
	for _, ref := range refs {
		if location := candidate.assetLocations[ref]; location != "" {
			locations[ref] = DiagnosticLocation{Path: location}
		}
	}
	return locations
}

func diagnosticLocation(path string) *DiagnosticLocation {
	if path == "" {
		return nil
	}
	return &DiagnosticLocation{Path: path}
}

func diagnosticLocationKey(location *DiagnosticLocation) string {
	if location == nil {
		return ""
	}
	return fmt.Sprintf("%s:%010d:%010d", location.Path, location.Line, location.Column)
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func semanticTypeForOssie(value ossie.DataType) expression.SemanticType {
	switch value {
	case ossie.DataTypeBoolean:
		return expression.TypeBoolean
	case ossie.DataTypeInteger:
		return expression.TypeInteger
	case ossie.DataTypeDecimal, ossie.DataTypeFloat:
		return expression.TypeDecimal
	case ossie.DataTypeString:
		return expression.TypeString
	case ossie.DataTypeDate:
		return expression.TypeDate
	case ossie.DataTypeTime:
		return expression.TypeTime
	case ossie.DataTypeDateTime, ossie.DataTypeDateTimeTz:
		return expression.TypeTimestamp
	case ossie.DataTypeOpaque:
		return expression.TypeVariant
	default:
		return expression.TypeUnknown
	}
}

func containsDifferentConcreteType(values []string, expected string) bool {
	for _, value := range values {
		if value != string(expression.TypeUnknown) && value != string(expression.TypeNull) && value != expected {
			return true
		}
	}
	return false
}

func uniqueConcrete(values []string, ignored ...string) []string {
	ignore := make(map[string]struct{}, len(ignored))
	for _, value := range ignored {
		ignore[value] = struct{}{}
	}
	var result []string
	for _, value := range values {
		if _, ok := ignore[value]; !ok {
			result = append(result, value)
		}
	}
	return result
}

func semanticTypes(values []string) []expression.SemanticType {
	out := make([]expression.SemanticType, len(values))
	for index, value := range values {
		out[index] = expression.SemanticType(value)
	}
	return out
}

func aggregationStates(values []string) []expression.AggregationState {
	out := make([]expression.AggregationState, len(values))
	for index, value := range values {
		out[index] = expression.AggregationState(value)
	}
	return out
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
