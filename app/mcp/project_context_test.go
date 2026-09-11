package mcp

import (
	"errors"
	"net/http"
	"testing"

	"github.com/meaningforge/metis/serrors"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResolveToolProjectIDPrecedence(t *testing.T) {
	request := &mcpsdk.CallToolRequest{
		Params: &mcpsdk.CallToolParamsRaw{Meta: mcpsdk.Meta{ProjectMetaKey: "from-meta"}},
		Extra:  &mcpsdk.RequestExtra{Header: http.Header{ProjectHeaderKey: []string{"from-header"}}},
	}
	for _, tt := range []struct {
		name     string
		explicit string
		request  *mcpsdk.CallToolRequest
		want     string
		source   projectContextSource
	}{
		{name: "argument", explicit: "from-argument", request: request, want: "from-argument", source: projectSourceArgument},
		{name: "meta", request: request, want: "from-meta", source: projectSourceMeta},
		{name: "header", request: &mcpsdk.CallToolRequest{Extra: request.Extra}, want: "from-header", source: projectSourceHeader},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, source, err := resolveToolProjectID(tt.explicit, tt.request)
			if err != nil || got != tt.want || source != tt.source {
				t.Fatalf("project=(%q,%q,%v), want (%q,%q,nil)", got, source, err, tt.want, tt.source)
			}
		})
	}
}

func TestResolveToolProjectIDFailsClosedWhenMissing(t *testing.T) {
	_, source, err := resolveToolProjectID("", nil)
	var semanticErr *serrors.Error
	if source != projectSourceMissing || !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrProjectRequired {
		t.Fatalf("source=%q err=%#v", source, err)
	}
}

func TestResolveToolProjectIDRejectsInvalidMetadataWithoutHeaderFallback(t *testing.T) {
	request := &mcpsdk.CallToolRequest{
		Params: &mcpsdk.CallToolParamsRaw{Meta: mcpsdk.Meta{ProjectMetaKey: 7}},
		Extra:  &mcpsdk.RequestExtra{Header: http.Header{ProjectHeaderKey: []string{"from-header"}}},
	}
	_, source, err := resolveToolProjectID("", request)
	var semanticErr *serrors.Error
	if source != projectSourceMeta || !errors.As(err, &semanticErr) || semanticErr.Code != serrors.ErrInvalidQuery {
		t.Fatalf("source=%q err=%#v", source, err)
	}
}
