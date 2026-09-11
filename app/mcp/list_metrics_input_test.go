package mcp

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestListMetricsInputMapsTypedMetaFilterToServiceRequest(t *testing.T) {
	limit := 7
	input := listMetricsInput{
		ProjectID: "ignored-by-adapter",
		Search:    listMetricsSearchTerms{"revenue", "sales"},
		MetaFilter: &listMetricsMetaFilter{Models: []string{
			"model:orders",
			"model:order_items",
			"model:orders",
		}},
		Limit: &limit,
	}

	request := input.serviceRequest("finance")
	if request.ProjectID != "finance" {
		t.Fatalf("project_id = %q, want finance", request.ProjectID)
	}
	if !reflect.DeepEqual(request.Search, []string(input.Search)) {
		t.Fatalf("search = %#v, want %#v", request.Search, input.Search)
	}
	if !reflect.DeepEqual(request.Models, input.MetaFilter.Models) {
		t.Fatalf("models = %#v, want %#v", request.Models, input.MetaFilter.Models)
	}
	if request.Limit != &limit {
		t.Fatalf("limit pointer changed: got %p want %p", request.Limit, &limit)
	}
}

func TestListMetricsInputAcceptsScalarOrListSearch(t *testing.T) {
	for name, raw := range map[string]string{
		"scalar": `{"search":"revenue"}`,
		"list":   `{"search":["revenue","sales"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var input listMetricsInput
			if err := json.Unmarshal([]byte(raw), &input); err != nil {
				t.Fatal(err)
			}
			request := input.serviceRequest("finance")
			if len(request.Search) == 0 || request.Search[0] != "revenue" {
				t.Fatalf("search = %#v", request.Search)
			}
			if name == "scalar" && len(request.Search) != 1 {
				t.Fatalf("scalar search = %#v", request.Search)
			}
			if name == "list" && !reflect.DeepEqual(request.Search, []string{"revenue", "sales"}) {
				t.Fatalf("list search = %#v", request.Search)
			}
		})
	}
}

func TestListMetricsInputRejectsInvalidSearchShape(t *testing.T) {
	var input listMetricsInput
	if err := json.Unmarshal([]byte(`{"search":42}`), &input); err == nil {
		t.Fatal("numeric search unexpectedly accepted")
	}
}

func TestListMetricsInputWithoutMetaFilterLeavesModelScopeOpen(t *testing.T) {
	request := (listMetricsInput{Search: listMetricsSearchTerms{"revenue"}}).serviceRequest("finance")
	if request.Models != nil {
		t.Fatalf("models = %#v, want nil", request.Models)
	}
}

func TestListMetricsInputCopiesSlices(t *testing.T) {
	input := listMetricsInput{
		Search:     listMetricsSearchTerms{"revenue"},
		MetaFilter: &listMetricsMetaFilter{Models: []string{"model:orders"}},
	}
	request := input.serviceRequest("finance")
	input.Search[0] = "changed"
	input.MetaFilter.Models[0] = "model:changed"
	if request.Search[0] != "revenue" || request.Models[0] != "model:orders" {
		t.Fatalf("service request aliases MCP input: %#v", request)
	}
}
