package semantic

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/meaningforge/metis/manifest"
)

// discoveryIndex is a disposable read model derived from exactly one immutable
// SemanticManifest. It narrows candidates only; manifest lookup, visibility,
// ranking, and semantic resolution remain authoritative.
type discoveryIndex struct {
	manifest *manifest.SemanticManifest
	projects map[string]*projectDiscoveryIndex
	bytes    int64
}

type projectDiscoveryIndex struct {
	models             []indexedModel
	modelSearch        substringIndex
	metrics            []indexedMetric
	metricSearch       substringIndex
	assets             []indexedSearchAsset
	assetSearch        substringIndex
	compatibilityEdges []indexedCompatibilityEdge
}

type indexedModel struct {
	name string
}

type indexedMetric struct {
	model           string
	name            string
	requiredMetrics []string
	sourceDatasets  []string
	constraints     []MetricSemanticConstraint
}

type indexedCompatibilityEdge struct {
	model        string
	relationship string
	from         string
	to           string
}

type indexedSearchAsset struct {
	kind      AssetKind
	model     string
	name      string
	qualified string
	dataset   string
}

// substringIndex maps normalized rune trigrams to sorted candidate ordinals.
// Short search terms deliberately fall back to the complete ordered candidate
// set so indexing never changes substring semantics.
type substringIndex struct {
	all      []int
	postings map[string][]int
}

func buildDiscoveryIndex(semanticManifest *manifest.SemanticManifest) (*discoveryIndex, error) {
	index := &discoveryIndex{manifest: semanticManifest, projects: map[string]*projectDiscoveryIndex{}}
	if semanticManifest == nil {
		return index, nil
	}
	projectNames := make([]string, 0, len(semanticManifest.Projects))
	for name := range semanticManifest.Projects {
		projectNames = append(projectNames, name)
	}
	sort.Strings(projectNames)
	for _, projectName := range projectNames {
		project := semanticManifest.Projects[projectName]
		if project == nil {
			continue
		}
		projectIndex := &projectDiscoveryIndex{}
		modelNames := make([]string, 0, len(project.Models))
		for name := range project.Models {
			modelNames = append(modelNames, name)
		}
		sort.Strings(modelNames)
		modelDocuments := make([]string, 0, len(modelNames))
		var metricDocuments []string
		var assetDocuments []string
		for _, modelName := range modelNames {
			model := project.Models[modelName]
			if model == nil || model.Model == nil {
				continue
			}
			projectIndex.models = append(projectIndex.models, indexedModel{name: modelName})
			modelValues := []string{modelName, modelAssetRef(modelName), model.Model.Description, searchableAIContext(model.Model.AIContext)}
			datasetNames := make([]string, 0, len(model.Datasets))
			for name := range model.Datasets {
				datasetNames = append(datasetNames, name)
			}
			sort.Strings(datasetNames)
			modelValues = append(modelValues, datasetNames...)
			metricNames := make([]string, 0, len(model.Metrics))
			for name := range model.Metrics {
				metricNames = append(metricNames, name)
			}
			sort.Strings(metricNames)
			modelValues = append(modelValues, metricNames...)
			fieldNames := make([]string, 0, len(model.Fields))
			for qualified := range model.Fields {
				fieldNames = append(fieldNames, qualified)
			}
			sort.Strings(fieldNames)
			modelValues = append(modelValues, fieldNames...)
			modelDocuments = append(modelDocuments, normalizeIndexDocument(modelValues...))
			projectIndex.assets = append(projectIndex.assets, indexedSearchAsset{kind: AssetModel, model: modelName, name: modelName})
			assetDocuments = append(assetDocuments, normalizeDiscoveryIndexDocument(modelName, modelName, model.Model.Description))

			for _, metricName := range metricNames {
				metric := model.Metrics[metricName]
				if metric == nil {
					continue
				}
				summary, err := agentMetricSummary(modelName, metric)
				if err != nil {
					return nil, err
				}
				constraints, err := metricSemanticConstraints(metric)
				if err != nil {
					return nil, err
				}
				values := append([]string{metricName, summary.Ref}, summary.Aliases...)
				values = append(values, modelName, metric.Description, searchableAIContext(metric.AIContext), strings.Join(metricConstraintSearchValues(constraints), " "))
				requiredMetrics := []string{metricName}
				if model.MetricDependencyGraph != nil {
					if order, orderErr := model.MetricDependencyGraph.EvaluationOrder(metricName); orderErr == nil {
						requiredMetrics = append([]string(nil), order...)
						foundSelected := false
						for _, name := range requiredMetrics {
							foundSelected = foundSelected || name == metricName
						}
						if !foundSelected {
							requiredMetrics = append(requiredMetrics, metricName)
						}
					}
				}
				sourceDatasets := []string(nil)
				if sources, sourceErr := model.MetricSources(metricName); sourceErr == nil {
					seenSources := make(map[string]struct{}, len(sources))
					for _, source := range sources {
						if _, exists := seenSources[source.Dataset]; !exists {
							seenSources[source.Dataset] = struct{}{}
							sourceDatasets = append(sourceDatasets, source.Dataset)
						}
					}
					sort.Strings(sourceDatasets)
				}
				projectIndex.metrics = append(projectIndex.metrics, indexedMetric{model: modelName, name: metricName, requiredMetrics: requiredMetrics, sourceDatasets: sourceDatasets, constraints: constraints})
				metricDocuments = append(metricDocuments, normalizeIndexDocument(values...))
				projectIndex.assets = append(projectIndex.assets, indexedSearchAsset{kind: AssetMetric, model: modelName, name: metricName, qualified: modelName + "." + metricName})
				assetDocuments = append(assetDocuments, normalizeDiscoveryIndexDocument(metricName, modelName+"."+metricName, metric.Description))
			}
			for _, datasetName := range datasetNames {
				dataset := model.Datasets[datasetName]
				if dataset == nil {
					continue
				}
				for fieldIndex := range dataset.Fields {
					field := &dataset.Fields[fieldIndex]
					if field.Dimension == nil {
						continue
					}
					qualified := datasetName + "." + field.Name
					projectIndex.assets = append(projectIndex.assets, indexedSearchAsset{kind: AssetDimension, model: modelName, name: field.Name, qualified: qualified, dataset: datasetName})
					assetDocuments = append(assetDocuments, normalizeDiscoveryIndexDocument(field.Name, qualified, field.Description))
				}
			}
			relationshipNames := make([]string, 0, len(model.Relationships))
			for name := range model.Relationships {
				relationshipNames = append(relationshipNames, name)
			}
			sort.Strings(relationshipNames)
			for _, name := range relationshipNames {
				relationship := model.Relationships[name]
				if relationship == nil {
					continue
				}
				projectIndex.assets = append(projectIndex.assets, indexedSearchAsset{kind: AssetRelationship, model: modelName, name: name, qualified: modelName + "." + name})
				assetDocuments = append(assetDocuments, normalizeDiscoveryIndexDocument(name, modelName+"."+name, relationship.From+" to "+relationship.To))
				projectIndex.compatibilityEdges = append(projectIndex.compatibilityEdges, indexedCompatibilityEdge{model: modelName, relationship: name, from: relationship.From, to: relationship.To})
			}
		}
		ontologyNames := make([]string, 0, len(project.Ontology))
		for name := range project.Ontology {
			ontologyNames = append(ontologyNames, name)
		}
		sort.Strings(ontologyNames)
		for _, name := range ontologyNames {
			concept := project.Ontology[name]
			if concept == nil {
				continue
			}
			projectIndex.assets = append(projectIndex.assets, indexedSearchAsset{kind: AssetOntologyConcept, name: concept.Name, qualified: "ontology." + concept.Name})
			searchText := concept.Type + " " + strings.Join(concept.Extends, " ")
			assetDocuments = append(assetDocuments, normalizeDiscoveryIndexDocument(concept.Name, "ontology."+concept.Name, strings.TrimSpace(concept.Description+" "+searchText)))
		}
		order := make([]int, len(projectIndex.assets))
		for ordinal := range order {
			order[ordinal] = ordinal
		}
		sort.Slice(order, func(i, j int) bool {
			return indexedSemanticSearchKey(projectIndex.assets[order[i]]) < indexedSemanticSearchKey(projectIndex.assets[order[j]])
		})
		orderedAssets := make([]indexedSearchAsset, len(order))
		orderedDocuments := make([]string, len(order))
		for ordinal, previous := range order {
			orderedAssets[ordinal] = projectIndex.assets[previous]
			orderedDocuments[ordinal] = assetDocuments[previous]
		}
		projectIndex.assets = orderedAssets
		assetDocuments = orderedDocuments
		projectIndex.modelSearch = newSubstringIndex(modelDocuments)
		projectIndex.metricSearch = newSubstringIndex(metricDocuments)
		projectIndex.assetSearch = newSubstringIndex(assetDocuments)
		index.projects[projectName] = projectIndex
	}
	index.bytes = index.approximateBytes()
	return index, nil
}

func indexedSemanticSearchKey(asset indexedSearchAsset) string {
	name := asset.name
	if asset.kind == AssetDimension {
		name = asset.qualified
	}
	return string(asset.kind) + "\x00" + asset.model + "\x00" + name
}

func normalizeDiscoveryIndexDocument(values ...string) string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if value = normalize(value); value != "" {
			normalized = append(normalized, value)
		}
	}
	return strings.Join(normalized, "\x00")
}

func normalizeIndexDocument(values ...string) string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if value = normalizeAgentSearchText(value); value != "" {
			normalized = append(normalized, value)
		}
	}
	return strings.Join(normalized, "\x00")
}

func newSubstringIndex(documents []string) substringIndex {
	index := substringIndex{all: make([]int, len(documents)), postings: map[string][]int{}}
	for ordinal, document := range documents {
		index.all[ordinal] = ordinal
		seen := make(map[string]struct{})
		runes := []rune(document)
		for position := 0; position+2 < len(runes); position++ {
			seen[string(runes[position:position+3])] = struct{}{}
		}
		for gram := range seen {
			index.postings[gram] = append(index.postings[gram], ordinal)
		}
	}
	return index
}

func (i substringIndex) candidates(terms []string) []int {
	if len(terms) == 0 {
		return i.all
	}
	selected := make(map[int]struct{})
	for _, term := range terms {
		grams := stringTrigrams(term)
		if len(grams) == 0 {
			return i.all
		}
		postings := i.postings[grams[0]]
		for _, gram := range grams[1:] {
			postings = intersectSortedOrdinals(postings, i.postings[gram])
			if len(postings) == 0 {
				break
			}
		}
		for _, ordinal := range postings {
			selected[ordinal] = struct{}{}
		}
	}
	out := make([]int, 0, len(selected))
	for ordinal := range selected {
		out = append(out, ordinal)
	}
	sort.Ints(out)
	return out
}

func stringTrigrams(value string) []string {
	runes := []rune(value)
	if len(runes) < 3 {
		return nil
	}
	grams := make([]string, 0, len(runes)-2)
	for index := 0; index+2 < len(runes); index++ {
		grams = append(grams, string(runes[index:index+3]))
	}
	return grams
}

func intersectSortedOrdinals(left, right []int) []int {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	out := make([]int, 0, min(len(left), len(right)))
	for leftIndex, rightIndex := 0, 0; leftIndex < len(left) && rightIndex < len(right); {
		switch {
		case left[leftIndex] < right[rightIndex]:
			leftIndex++
		case left[leftIndex] > right[rightIndex]:
			rightIndex++
		default:
			out = append(out, left[leftIndex])
			leftIndex++
			rightIndex++
		}
	}
	return out
}

func (i *discoveryIndex) approximateBytes() int64 {
	if i == nil {
		return 0
	}
	bytes := int64(unsafe.Sizeof(*i))
	for projectName, project := range i.projects {
		bytes += int64(len(projectName)) + int64(unsafe.Sizeof(*project))
		bytes += int64(cap(project.models)) * int64(unsafe.Sizeof(indexedModel{}))
		for _, model := range project.models {
			bytes += int64(len(model.name))
		}
		bytes += int64(cap(project.metrics)) * int64(unsafe.Sizeof(indexedMetric{}))
		for _, metric := range project.metrics {
			bytes += int64(len(metric.model) + len(metric.name))
			bytes += int64(cap(metric.requiredMetrics)+cap(metric.sourceDatasets)) * int64(unsafe.Sizeof(""))
			for _, name := range metric.requiredMetrics {
				bytes += int64(len(name))
			}
			for _, name := range metric.sourceDatasets {
				bytes += int64(len(name))
			}
			bytes += int64(cap(metric.constraints)) * int64(unsafe.Sizeof(MetricSemanticConstraint{}))
		}
		bytes += int64(cap(project.assets)) * int64(unsafe.Sizeof(indexedSearchAsset{}))
		for _, asset := range project.assets {
			bytes += int64(len(asset.kind) + len(asset.model) + len(asset.name) + len(asset.qualified) + len(asset.dataset))
		}
		bytes += int64(cap(project.compatibilityEdges)) * int64(unsafe.Sizeof(indexedCompatibilityEdge{}))
		bytes += approximateSubstringIndexBytes(project.modelSearch) + approximateSubstringIndexBytes(project.metricSearch) + approximateSubstringIndexBytes(project.assetSearch)
	}
	return bytes
}

func approximateSubstringIndexBytes(index substringIndex) int64 {
	bytes := int64(cap(index.all)) * int64(unsafe.Sizeof(int(0)))
	for gram, postings := range index.postings {
		bytes += int64(len(gram)) + int64(cap(postings))*int64(unsafe.Sizeof(int(0)))
	}
	return bytes
}

type discoveryIndexCache struct {
	mu    sync.Mutex
	value atomic.Pointer[discoveryIndex]
}
