package dbt_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meaningforge/metis/ossie"
)

type diagnostic struct {
	Data struct {
		Issue   string `json:"issue_type"`
		Element string `json:"element_name"`
	} `json:"data"`
	Info struct {
		Code string `json:"code"`
	} `json:"info"`
}

type receipt struct {
	Exit         int          `json:"dbt_exit"`
	OSIHash      string       `json:"osi_sha256"`
	ManifestHash string       `json:"semantic_manifest_sha256"`
	SourceHash   string       `json:"source_sha256"`
	Repeat       bool         `json:"repeat_osi_equal"`
	MetisExit    int          `json:"metis_exit"`
	Compile      string       `json:"metis_compile"`
	Present      bool         `json:"osi_present"`
	Diagnostics  []diagnostic `json:"diagnostics"`
	Stale        struct {
		Exit      int  `json:"exit"`
		Present   bool `json:"old_osi_still_present"`
		Unchanged bool `json:"old_osi_unchanged"`
	} `json:"failed_after_success"`
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func decode(t *testing.T, path string, out any) {
	t.Helper()
	if err := json.Unmarshal(read(t, path), out); err != nil {
		t.Fatal(err)
	}
}

func TestCapturedNativeArtifactsAndAcceptance(t *testing.T) {
	var evidence struct {
		Commit   string             `json:"metis_commit"`
		Versions map[string]string  `json:"versions"`
		Cases    map[string]receipt `json:"cases"`
	}
	decode(t, "evidence/acceptance.json", &evidence)
	if evidence.Commit != "1866998401d8174719d7cddc49c5b295531a5e63" || evidence.Versions["dbt-core"] != "1.12.0" {
		t.Fatal("capture provenance drift")
	}
	for _, name := range []string{"basic", "window", "visibility"} {
		t.Run(name, func(t *testing.T) {
			r := evidence.Cases[name]
			if r.Exit != 0 || !r.Repeat || r.MetisExit != 1 || r.Compile != "NOT_EXECUTED" {
				t.Fatalf("invalid acceptance receipt: %+v", r)
			}
			for file, want := range map[string]string{"osi_document.json": r.OSIHash, "semantic_manifest.json": r.ManifestHash, "semantic.yml": r.SourceHash} {
				sum := sha256.Sum256(read(t, filepath.Join("evidence", name, file)))
				if hex.EncodeToString(sum[:]) != want {
					t.Fatalf("%s differs from native capture", file)
				}
			}
			b := read(t, filepath.Join("evidence", name, "osi_document.json"))
			var doc struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(b, &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Version != "0.1.1" {
				t.Fatalf("unexpected native version: %s", doc.Version)
			}
			// Do not rewrite the version, delete dialects or infer missing types.
			if _, err := ossie.NewLoader().Load(b); err == nil || !strings.Contains(err.Error(), "field dialects not found") {
				t.Fatalf("native load boundary changed; re-execute compatibility acceptance: %v", err)
			}
		})
	}
	invalid := evidence.Cases["invalid"]
	if invalid.Exit == 0 || invalid.Present || invalid.Compile != "NOT_EXECUTED" {
		t.Fatal("fresh invalid generation must not be consumed")
	}
	stale := evidence.Cases["basic"].Stale
	if stale.Exit == 0 || !stale.Present || !stale.Unchanged {
		t.Fatal("failed generation stale-file discriminator lost")
	}
	for name, wants := range map[string]map[string]string{
		"window":     {"rolling_revenue": "CUMULATIVE_SEMANTICS_LOSS", "monthly_revenue": "CUMULATIVE_SEMANTICS_LOSS"},
		"visibility": {"private_revenue": "PRIVATE_METRIC_DROPPED", "customer": "NATURAL_ENTITY_DROPPED"},
	} {
		for _, d := range evidence.Cases[name].Diagnostics {
			if d.Info.Code == "I078" && wants[d.Data.Element] == d.Data.Issue {
				delete(wants, d.Data.Element)
			}
		}
		if len(wants) != 0 {
			t.Fatalf("missing native %s loss diagnostics: %v", name, wants)
		}
	}
}

type expression struct {
	Dialects []struct {
		SQL string `json:"expression"`
	} `json:"dialects"`
}
type nativeModel struct {
	Datasets []struct {
		Name   string `json:"name"`
		Fields []struct {
			Name     string  `json:"name"`
			Datatype *string `json:"datatype"`
		} `json:"fields"`
	} `json:"datasets"`
	Metrics []struct {
		Name       string     `json:"name"`
		Expression expression `json:"expression"`
	} `json:"metrics"`
	Relationships []json.RawMessage `json:"relationships"`
}

func loadModel(t *testing.T, name string) nativeModel {
	t.Helper()
	var doc struct {
		Models []nativeModel `json:"semantic_model"`
	}
	decode(t, filepath.Join("evidence", name, "osi_document.json"), &doc)
	if len(doc.Models) != 1 {
		t.Fatal("expected one native model")
	}
	return doc.Models[0]
}

func TestNativeRepresentationLossDiscriminators(t *testing.T) {
	basic := loadModel(t, "basic")
	if len(basic.Relationships) != 1 || len(basic.Metrics) != 1 || basic.Metrics[0].Name != "revenue" || basic.Metrics[0].Expression.Dialects[0].SQL != "SUM(orders.amount)" {
		t.Fatal("basic native identity/relationship changed")
	}
	for _, dataset := range basic.Datasets {
		for _, field := range dataset.Fields {
			if field.Datatype != nil || field.Name == "amount" {
				t.Fatal("re-evaluate native missing-type/measure gap")
			}
		}
	}
	window := loadModel(t, "window")
	if len(window.Metrics) != 3 {
		t.Fatal("expected preserved but downgraded metrics")
	}
	for _, metric := range window.Metrics {
		if len(metric.Expression.Dialects) != 1 || metric.Expression.Dialects[0].SQL != "SUM(orders.amount)" {
			t.Fatal("native cumulative downgrade changed")
		}
	}
	var source struct {
		Metrics []struct {
			Name   string `json:"name"`
			Type   string `json:"type"`
			Params struct {
				Private    bool `json:"is_private"`
				Cumulative struct {
					Window *struct {
						Count int    `json:"count"`
						Grain string `json:"granularity"`
					} `json:"window"`
					Grain string `json:"grain_to_date"`
				} `json:"cumulative_type_params"`
			} `json:"type_params"`
		} `json:"metrics"`
	}
	decode(t, "evidence/window/semantic_manifest.json", &source)
	checked := 0
	for _, metric := range source.Metrics {
		if metric.Name == "rolling_revenue" {
			w := metric.Params.Cumulative.Window
			if metric.Type != "cumulative" || w == nil || w.Count != 3 || w.Grain != "day" {
				t.Fatal("source rolling discriminator lost")
			}
			checked++
		}
		if metric.Name == "monthly_revenue" {
			if metric.Params.Cumulative.Grain != "month" {
				t.Fatal("source month discriminator lost")
			}
			checked++
		}
	}
	if checked != 2 {
		t.Fatal("missing source cumulative metrics")
	}
	decode(t, "evidence/visibility/semantic_manifest.json", &source)
	private := false
	for _, metric := range source.Metrics {
		if metric.Name == "private_revenue" && metric.Params.Private {
			private = true
		}
	}
	if !private {
		t.Fatal("source visibility discriminator lost")
	}
	visibility := loadModel(t, "visibility")
	if len(visibility.Metrics) != 1 || visibility.Metrics[0].Name != "revenue" || len(visibility.Relationships) != 0 {
		t.Fatal("dropped private metric/natural relationship boundary changed")
	}
}
