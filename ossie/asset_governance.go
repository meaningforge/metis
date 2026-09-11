package ossie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const AssetGovernanceExtensionKind = "asset_governance"

type AssetLifecycle string

const (
	AssetLifecycleActive     AssetLifecycle = "active"
	AssetLifecycleDeprecated AssetLifecycle = "deprecated"
)

type AssetCertification string

const (
	AssetCertificationUncertified AssetCertification = "uncertified"
	AssetCertificationCertified   AssetCertification = "certified"
)

// AssetGovernance is the typed Metis projection of governance evidence carried
// in an Ossie custom extension. Zero authored metadata is represented by the
// active/uncertified defaults returned by EffectiveAssetGovernance.
type AssetGovernance struct {
	Kind            string             `json:"kind"`
	Owner           string             `json:"owner,omitempty"`
	Lifecycle       AssetLifecycle     `json:"lifecycle,omitempty"`
	Certification   AssetCertification `json:"certification,omitempty"`
	DeprecationDate string             `json:"deprecation_date,omitempty"`
	Replacement     string             `json:"replacement,omitempty"`
	PolicyTags      []string           `json:"policy_tags,omitempty"`
}

var assetPolicyTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,63}$`)

// ParseAssetGovernance extracts one typed METIS asset_governance extension.
// Extensions from other vendors and METIS extension kinds remain opaque.
func ParseAssetGovernance(extensions []CustomExtension) (AssetGovernance, bool, error) {
	var data string
	for _, extension := range extensions {
		if extension.VendorName != MetisExtensionVendor {
			continue
		}
		var envelope struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(extension.Data), &envelope); err != nil {
			return AssetGovernance{}, false, fmt.Errorf("decode METIS asset extension: %w", err)
		}
		if envelope.Kind != AssetGovernanceExtensionKind {
			continue
		}
		if data != "" {
			return AssetGovernance{}, false, fmt.Errorf("duplicate METIS asset_governance extension")
		}
		data = extension.Data
	}
	if data == "" {
		return AssetGovernance{}, false, nil
	}
	decoder := json.NewDecoder(bytes.NewBufferString(data))
	decoder.DisallowUnknownFields()
	var governance AssetGovernance
	if err := decoder.Decode(&governance); err != nil {
		return AssetGovernance{}, false, fmt.Errorf("decode METIS asset_governance extension: %w", err)
	}
	if err := ValidateAssetGovernance(governance); err != nil {
		return AssetGovernance{}, false, err
	}
	governance.Owner = strings.TrimSpace(governance.Owner)
	governance.Replacement = strings.TrimSpace(governance.Replacement)
	governance.PolicyTags = append([]string(nil), governance.PolicyTags...)
	sort.Strings(governance.PolicyTags)
	return governance, true, nil
}

func EffectiveAssetGovernance(extensions []CustomExtension) (AssetGovernance, error) {
	governance, ok, err := ParseAssetGovernance(extensions)
	if err != nil {
		return AssetGovernance{}, err
	}
	if !ok {
		return AssetGovernance{Kind: AssetGovernanceExtensionKind, Lifecycle: AssetLifecycleActive, Certification: AssetCertificationUncertified}, nil
	}
	if governance.Lifecycle == "" {
		governance.Lifecycle = AssetLifecycleActive
	}
	if governance.Certification == "" {
		governance.Certification = AssetCertificationUncertified
	}
	return governance, nil
}

func ValidateAssetGovernance(governance AssetGovernance) error {
	if governance.Kind != AssetGovernanceExtensionKind {
		return fmt.Errorf("asset governance kind must be %q", AssetGovernanceExtensionKind)
	}
	if strings.TrimSpace(governance.Owner) != governance.Owner || len(governance.Owner) > 256 {
		return fmt.Errorf("asset governance owner must be trimmed and at most 256 characters")
	}
	switch governance.Lifecycle {
	case "", AssetLifecycleActive, AssetLifecycleDeprecated:
	default:
		return fmt.Errorf("unsupported asset governance lifecycle %q", governance.Lifecycle)
	}
	switch governance.Certification {
	case "", AssetCertificationUncertified, AssetCertificationCertified:
	default:
		return fmt.Errorf("unsupported asset governance certification %q", governance.Certification)
	}
	if governance.DeprecationDate != "" {
		if governance.Lifecycle != AssetLifecycleDeprecated {
			return fmt.Errorf("asset governance deprecation_date requires deprecated lifecycle")
		}
		if _, err := time.Parse("2006-01-02", governance.DeprecationDate); err != nil {
			return fmt.Errorf("asset governance deprecation_date must use YYYY-MM-DD")
		}
	}
	if strings.TrimSpace(governance.Replacement) != governance.Replacement {
		return fmt.Errorf("asset governance replacement must be trimmed")
	}
	if governance.Replacement != "" && governance.Lifecycle != AssetLifecycleDeprecated {
		return fmt.Errorf("asset governance replacement requires deprecated lifecycle")
	}
	if len(governance.PolicyTags) > 32 {
		return fmt.Errorf("asset governance policy_tags accepts at most 32 values")
	}
	seen := make(map[string]struct{}, len(governance.PolicyTags))
	for _, tag := range governance.PolicyTags {
		if !assetPolicyTagPattern.MatchString(tag) {
			return fmt.Errorf("invalid asset governance policy tag %q", tag)
		}
		if _, ok := seen[tag]; ok {
			return fmt.Errorf("duplicate asset governance policy tag %q", tag)
		}
		seen[tag] = struct{}{}
	}
	return nil
}

type governedAsset struct {
	ref        string
	kind       string
	extensions []CustomExtension
}

func validateAssetGovernanceDocument(doc *Document) error {
	assets := make(map[string]governedAsset)
	for modelIndex := range doc.SemanticModel {
		model := &doc.SemanticModel[modelIndex]
		entries := []governedAsset{{ref: "model:" + model.Name, kind: "model", extensions: model.CustomExtensions}}
		for datasetIndex := range model.Datasets {
			dataset := &model.Datasets[datasetIndex]
			entries = append(entries, governedAsset{ref: "dataset:" + model.Name + "." + dataset.Name, kind: "dataset", extensions: dataset.CustomExtensions})
			for fieldIndex := range dataset.Fields {
				field := &dataset.Fields[fieldIndex]
				if field.Dimension != nil {
					entries = append(entries, governedAsset{ref: "dimension:" + model.Name + "." + dataset.Name + "." + field.Name, kind: "dimension", extensions: field.CustomExtensions})
				} else if _, ok, err := ParseAssetGovernance(field.CustomExtensions); err != nil || ok {
					if err != nil {
						return fmt.Errorf("field %s.%s.%s: %w", model.Name, dataset.Name, field.Name, err)
					}
					return fmt.Errorf("field %s.%s.%s: asset_governance is supported only for dimension fields", model.Name, dataset.Name, field.Name)
				}
			}
		}
		for metricIndex := range model.Metrics {
			metric := &model.Metrics[metricIndex]
			entries = append(entries, governedAsset{ref: "metric:" + model.Name + "." + metric.Name, kind: "metric", extensions: metric.CustomExtensions})
		}
		for relationshipIndex := range model.Relationships {
			relationship := &model.Relationships[relationshipIndex]
			entries = append(entries, governedAsset{ref: "relationship:" + model.Name + "." + relationship.Name, kind: "relationship", extensions: relationship.CustomExtensions})
		}
		for _, entry := range entries {
			assets[entry.ref] = entry
		}
	}
	replacements := make(map[string]string)
	for ref, asset := range assets {
		governance, ok, err := ParseAssetGovernance(asset.extensions)
		if err != nil {
			return fmt.Errorf("%s: %w", ref, err)
		}
		if !ok || governance.Replacement == "" {
			continue
		}
		target, exists := assets[governance.Replacement]
		if !exists {
			return fmt.Errorf("%s: replacement %q does not exist", ref, governance.Replacement)
		}
		if target.kind != asset.kind {
			return fmt.Errorf("%s: replacement %q must reference the same asset kind", ref, governance.Replacement)
		}
		if governance.Replacement == ref {
			return fmt.Errorf("%s: replacement cannot reference itself", ref)
		}
		replacements[ref] = governance.Replacement
	}
	state := make(map[string]uint8, len(replacements))
	var visit func(string) error
	visit = func(ref string) error {
		switch state[ref] {
		case 1:
			return fmt.Errorf("asset governance replacement cycle contains %q", ref)
		case 2:
			return nil
		}
		state[ref] = 1
		if next := replacements[ref]; next != "" {
			if err := visit(next); err != nil {
				return err
			}
		}
		state[ref] = 2
		return nil
	}
	refs := make([]string, 0, len(replacements))
	for ref := range replacements {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		if err := visit(ref); err != nil {
			return err
		}
	}
	return nil
}
