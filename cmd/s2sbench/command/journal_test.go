package command

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

func TestRunJournalSyncsEachRecordAndRetainsPartialEvidence(t *testing.T) {
	output := filepath.Join(t.TempDir(), "run-001")
	manifest := s2sbench.RunManifest{SchemaVersion: "test"}
	journal, err := newRunJournal(output, manifest)
	if err != nil {
		t.Fatal(err)
	}
	record := s2sbench.AttemptRecord{
		Path:       s2sbench.PathRawAssets,
		Scenario:   "scenario",
		Repetition: 1,
		Index:      1,
		Question:   "question",
		Prompt:     "prompt",
		Transcript: `{"type":"agent-event"}`,
		StartedAt:  time.Now().UTC(),
		DurationMS: 42,
		SQL:        "SELECT \"order_amount\"\nFROM \"analytics\".\"orders\"",
	}
	if err := journal.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("final output exists before commit: %v", err)
	}
	file, err := os.Open(output + ".partial/attempts.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var got s2sbench.AttemptRecord
	if err := json.NewDecoder(bufio.NewReader(file)).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Question != record.Question || got.SQL != record.SQL || got.DurationMS != record.DurationMS {
		t.Fatalf("journal record = %#v, want %#v", got, record)
	}
	if got.Prompt != "" || got.Transcript != "" {
		t.Fatalf("journal persisted verbose interaction context: prompt=%q transcript=%q", got.Prompt, got.Transcript)
	}
	audit, err := os.ReadFile(output + ".partial/attempts.log")
	if err != nil {
		t.Fatal(err)
	}
	wantAudit, err := formatAttemptAuditLog(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(audit) != wantAudit+"\n" {
		t.Fatalf("audit log = %q, want %q", audit, wantAudit+"\n")
	}
	sqlPath := output + ".partial/sql/raw_assets/scenario/repetition-01-attempt-01.sql"
	sql, err := os.ReadFile(sqlPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(sql), record.SQL+"\n"; got != want {
		t.Fatalf("readable SQL evidence = %q, want %q", got, want)
	}
	if strings.Contains(string(sql), `\"`) {
		t.Fatalf("readable SQL evidence contains JSON escaping: %q", sql)
	}
}

func TestRunJournalSkipsReadableSQLFileWhenAttemptHasNoSQL(t *testing.T) {
	output := filepath.Join(t.TempDir(), "run-001")
	journal, err := newRunJournal(output, s2sbench.RunManifest{SchemaVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Append(s2sbench.AttemptRecord{
		Path: s2sbench.PathMetis, Scenario: "scenario", Repetition: 1, Index: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output + ".partial/sql"); !os.IsNotExist(err) {
		t.Fatalf("SQL evidence directory exists for empty SQL: %v", err)
	}
}

func TestResumeRunJournalRequiresIdenticalManifestAndAppendsEvidence(t *testing.T) {
	output := filepath.Join(t.TempDir(), "run-resume")
	manifest := s2sbench.RunManifest{SchemaVersion: "test", PromptVersion: "prompt"}
	journal, err := newRunJournal(output, manifest)
	if err != nil {
		t.Fatal(err)
	}
	first := s2sbench.AttemptRecord{Path: s2sbench.PathMetis, Scenario: "first", Repetition: 1, Index: 1, Question: "q1"}
	if err := journal.Append(first); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}

	resumed, records, err := resumeRunJournal(output, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Scenario != "first" {
		t.Fatalf("resumed records = %#v", records)
	}
	second := s2sbench.AttemptRecord{Path: s2sbench.PathMetis, Scenario: "second", Repetition: 1, Index: 1, Question: "q2"}
	if err := resumed.Append(second); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	all, err := readStrictJSONL[s2sbench.AttemptRecord](output + ".partial/attempts.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[1].Scenario != "second" {
		t.Fatalf("appended records = %#v", all)
	}

	drifted := manifest
	drifted.PromptVersion = "other"
	if _, _, err := resumeRunJournal(output, drifted); err == nil || !strings.Contains(err.Error(), "manifest differs") {
		t.Fatalf("drifted resume error = %v", err)
	}
}

func TestRunJournalPersistsEveryTurnTranscriptForTerminalToolOverrun(t *testing.T) {
	output := filepath.Join(t.TempDir(), "run-overrun")
	journal, err := newRunJournal(output, s2sbench.RunManifest{SchemaVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	record := s2sbench.AttemptRecord{
		Path:              s2sbench.PathMetis,
		Scenario:          "scenario",
		Repetition:        1,
		Index:             2,
		Question:          "question",
		Verdict:           s2sbench.VerdictFailed,
		Error:             "external agent exceeded tool-call budget: used 31, budget 30",
		PersistTranscript: true,
		Trace: []s2sbench.AttemptEvidence{
			{Index: 1, Transcript: "{\"turn\":1}"},
			{Index: 2, Transcript: "{\"turn\":2,\"tool\":31}\n"},
		},
	}
	if err := journal.Append(record); err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	for index, want := range []string{"{\"turn\":1}\n", "{\"turn\":2,\"tool\":31}\n"} {
		path := filepath.Join(output+".partial", "traces", "metis", "scenario", fmt.Sprintf("repetition-01-attempt-%02d.jsonl", index+1))
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read terminal trace %d: %v", index+1, err)
		}
		if string(got) != want {
			t.Fatalf("terminal trace %d = %q, want %q", index+1, got, want)
		}
	}
}
