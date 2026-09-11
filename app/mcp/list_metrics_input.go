package mcp

import (
	"encoding/json"
	"fmt"

	service "github.com/meaningforge/metis/app/service/semantic"
)

// listMetricsSearchTerms accepts the dbt-style convenience shape of either one
// search string or a list of search strings while normalizing both to the
// service layer's deterministic []string representation.
type listMetricsSearchTerms []string

func (terms *listMetricsSearchTerms) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*terms = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("list_metrics search must be a string or an array of strings: %w", err)
	}
	*terms = many
	return nil
}

// listMetricsMetaFilter keeps deterministic manifest narrowing separate from
// lightweight search. Keep this typed instead of accepting a free-form map so
// the MCP contract stays explicit as additional filters are introduced.
type listMetricsMetaFilter struct {
	Models []string `json:"models,omitempty"`
}

// listMetricsInput is the Agent-facing MCP request. Search is lightweight
// resource-specific retrieval; meta_filter is deterministic manifest narrowing.
type listMetricsInput struct {
	ProjectID  string                 `json:"project_id,omitempty"`
	Search     listMetricsSearchTerms `json:"search,omitempty"`
	MetaFilter *listMetricsMetaFilter `json:"meta_filter,omitempty"`
	Limit      *int                   `json:"limit,omitempty"`
	Cursor     string                 `json:"cursor,omitempty"`
}

var listMetricsInputSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "project_id": {
      "type": "string",
      "description": "Explicit project override only. Omit when an active project is configured; never use a model name."
    },
    "search": {
      "description": "Optional lightweight term or OR-list matched against metric and model identity, governed aliases, typed semantic constraints, description, and authored AI context. Ranking is retrieval-only and never selects a metric.",
      "anyOf": [
        {"type": "string"},
        {"type": "array", "items": {"type": "string"}}
      ]
    },
    "meta_filter": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "models": {
          "type": "array",
          "items": {"type": "string"},
          "description": "Optional canonical model refs returned by list_models. Metrics from any selected model are returned; refs are deduplicated."
        }
      }
    },
    "limit": {
      "type": "integer",
      "minimum": 1,
      "maximum": 50,
      "description": "Maximum metrics to return; defaults to 10 and cannot exceed 50."
    },
    "cursor": {
      "type": "string",
      "description": "Opaque next_cursor returned by an earlier request with the same filters and visibility scope."
    }
  },
  "additionalProperties": false
}`)

func (input listMetricsInput) serviceRequest(projectID string) service.ListMetricsRequest {
	request := service.ListMetricsRequest{
		ProjectID: projectID,
		Search:    append([]string(nil), input.Search...),
		Limit:     input.Limit,
		Cursor:    input.Cursor,
	}
	if input.MetaFilter != nil {
		request.Models = append([]string(nil), input.MetaFilter.Models...)
	}
	return request
}
