package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/meaningforge/metis/tests/conformance/evidence"
)

func TestSelectTargetsReturnsSortedRegisteredTargetsForAll(t *testing.T) {
	targets := []evidence.Target{{Dialect: "DORIS"}, {Dialect: "CLICKHOUSE"}}
	got, err := selectTargets(targets, "all")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"clickhouse", "doris"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectTargets(all) = %v, want %v", got, want)
	}
}

func TestSelectTargetsReturnsOneRegisteredTarget(t *testing.T) {
	targets := []evidence.Target{{Dialect: "DORIS"}, {Dialect: "CLICKHOUSE"}}
	got, err := selectTargets(targets, " ClickHouse ")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"clickhouse"}) {
		t.Fatalf("selectTargets(clickhouse) = %v", got)
	}
}

func TestSelectTargetsSupportsMultipleTargetsWithTrimDedupAndStableOrder(t *testing.T) {
	targets := []evidence.Target{{Dialect: "DORIS"}, {Dialect: "CLICKHOUSE"}, {Dialect: "SNOWFLAKE"}}
	got, err := selectTargets(targets, " doris, clickhouse,doris ")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"clickhouse", "doris"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selectTargets(multiple) = %v, want %v", got, want)
	}
}

func TestSelectTargetsFailsClosedForUnknownTarget(t *testing.T) {
	_, err := selectTargets([]evidence.Target{{Dialect: "DORIS"}}, "doris,snowflake")
	if err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("selectTargets unknown error = %v", err)
	}
}

func TestSelectTargetsRejectsMalformedSelectors(t *testing.T) {
	targets := []evidence.Target{{Dialect: "DORIS"}, {Dialect: "CLICKHOUSE"}}
	for _, selector := range []string{"", "doris,,clickhouse", "all,doris"} {
		if _, err := selectTargets(targets, selector); err == nil {
			t.Fatalf("selectTargets(%q) succeeded", selector)
		}
	}
}

func TestSelectTargetsFailsClosedForInvalidRegistry(t *testing.T) {
	if _, err := selectTargets(nil, "all"); err == nil {
		t.Fatal("selectTargets empty registry succeeded")
	}
	if _, err := selectTargets([]evidence.Target{{Dialect: ""}}, "all"); err == nil {
		t.Fatal("selectTargets empty dialect succeeded")
	}
	if _, err := selectTargets([]evidence.Target{{Dialect: "DORIS"}, {Dialect: "DORIS"}}, "all"); err == nil {
		t.Fatal("selectTargets duplicate dialect succeeded")
	}
}
