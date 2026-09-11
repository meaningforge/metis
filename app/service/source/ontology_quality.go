package source

import (
	"strings"

	"github.com/meaningforge/metis/serrors"
)

// Ontology invalidity is structural, not an advisory quality rule that can be
// downgraded by deployment severity overrides. Unsupported valid shapes remain
// preserved and advisory, subject to the ordinary publication threshold.
func appendOntologyQuality(candidate *ProjectSource, report *QualityReport) {
	for _, project := range candidate.Manifest.Projects {
		if project.OntologyResolution == nil {
			continue
		}
		for _, finding := range project.OntologyResolution.Diagnostics() {
			severity := SeverityWarning
			if finding.Blocking {
				severity = SeverityError
			}
			location := candidate.assetLocations["ontology_concept:"+strings.ToLower(finding.Concept)]
			if strings.HasPrefix(finding.Location, "ontology_mappings[") {
				prefix := strings.SplitN(finding.Location, ".", 2)[0]
				location = candidate.assetLocations[prefix]
			}
			report.Diagnostics = append(report.Diagnostics, Diagnostic{Code: DiagnosticCode(finding.Code), Severity: severity,
				Asset: "ontology_concept:" + finding.Concept, Location: diagnosticLocation(location),
				Message: "ontology mapping or graph validation: " + finding.Code, CallerAction: serrors.CallerActionChangeModel,
				Evidence: &DiagnosticEvidence{Reason: DiagnosticReason(strings.ToLower(finding.Code))},
			})
			if finding.Blocking || severityRank(severity) <= severityRank(report.PublicationThreshold) {
				report.Publishable = false
			}
		}
	}
}
