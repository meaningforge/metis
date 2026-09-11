package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	s2sbench "github.com/meaningforge/metis/cmd/s2sbench/bench"
)

type runJournal struct {
	mu      sync.Mutex
	output  string
	partial string
	stream  *incrementalJSONL[s2sbench.AttemptRecord]
	audit   *os.File
	closed  bool
}

func newRunJournal(output string, manifest s2sbench.RunManifest) (*runJournal, error) {
	partial, stream, err := createIncrementalBundle[s2sbench.RunManifest, s2sbench.AttemptRecord](output, manifest)
	if err != nil {
		return nil, err
	}
	audit, err := os.OpenFile(filepath.Join(partial, "attempts.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		_ = stream.Close()
		_ = os.RemoveAll(partial)
		return nil, fmt.Errorf("create incremental audit log: %w", err)
	}
	return &runJournal{output: output, partial: partial, stream: stream, audit: audit}, nil
}

func resumeRunJournal(output string, manifest s2sbench.RunManifest) (*runJournal, []s2sbench.AttemptRecord, error) {
	partial, stream, records, err := resumeIncrementalBundle[s2sbench.RunManifest, s2sbench.AttemptRecord](output, manifest)
	if err != nil {
		return nil, nil, err
	}
	audit, err := os.OpenFile(filepath.Join(partial, "attempts.log"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("open incremental audit log for resume: %w", err)
	}
	return &runJournal{output: output, partial: partial, stream: stream, audit: audit}, records, nil
}

func (j *runJournal) Append(record s2sbench.AttemptRecord) error {
	if j == nil {
		return fmt.Errorf("run journal is required")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.stream == nil {
		return fmt.Errorf("run journal is closed")
	}
	sqlPath, err := sqlEvidencePath(j.partial, record)
	if err != nil {
		return err
	}
	auditLine, err := formatAttemptAuditLog(record)
	if err != nil {
		return err
	}
	if record.PersistTranscript {
		if err := ensureTerminalTraceFiles(j.partial, record); err != nil {
			return fmt.Errorf("write terminal tool-overrun trace: %w", err)
		}
	}
	if err := j.stream.Append(record); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(j.audit, auditLine); err != nil {
		return fmt.Errorf("append incremental audit log: %w", err)
	}
	if err := j.audit.Sync(); err != nil {
		return fmt.Errorf("sync incremental audit log: %w", err)
	}
	if sqlPath != "" {
		if err := ensureSQLFile(sqlPath, record.SQL); err != nil {
			return fmt.Errorf("write readable SQL evidence: %w", err)
		}
	}
	return nil
}

func ensureTerminalTraceFiles(root string, record s2sbench.AttemptRecord) error {
	if record.Path != s2sbench.PathRawAssets && record.Path != s2sbench.PathMetis {
		return fmt.Errorf("terminal trace has unknown path %q", record.Path)
	}
	if !agentNamePattern.MatchString(record.Scenario) {
		return fmt.Errorf("terminal trace has unsafe scenario %q", record.Scenario)
	}
	if record.Repetition < 1 || record.Index < 1 {
		return fmt.Errorf("terminal trace identity is incomplete")
	}
	traces := record.Trace
	if len(traces) == 0 && strings.TrimSpace(record.Transcript) != "" {
		traces = []s2sbench.AttemptEvidence{{Index: record.Index, Transcript: record.Transcript}}
	}
	for _, evidence := range traces {
		if strings.TrimSpace(evidence.Transcript) == "" {
			continue
		}
		if evidence.Index < 1 || evidence.Index > record.Index {
			return fmt.Errorf("terminal trace attempt index %d is outside 1..%d", evidence.Index, record.Index)
		}
		name := fmt.Sprintf("repetition-%02d-attempt-%02d.jsonl", record.Repetition, evidence.Index)
		path := filepath.Join(root, "traces", string(record.Path), record.Scenario, name)
		if err := ensureTraceFile(path, evidence.Transcript); err != nil {
			return err
		}
	}
	return nil
}

func ensureTraceFile(path, transcript string) error {
	body := []byte(transcript)
	if !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, body) {
			return fmt.Errorf("existing terminal trace %q differs from attempt journal", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect terminal trace %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create terminal trace directory: %w", err)
	}
	return writeFile(path, func(w io.Writer) error {
		_, err := w.Write(body)
		return err
	})
}

func sqlEvidencePath(root string, record s2sbench.AttemptRecord) (string, error) {
	if strings.TrimSpace(record.SQL) == "" {
		return "", nil
	}
	if record.Path != s2sbench.PathRawAssets && record.Path != s2sbench.PathMetis {
		return "", fmt.Errorf("readable SQL evidence has unknown path %q", record.Path)
	}
	if !agentNamePattern.MatchString(record.Scenario) {
		return "", fmt.Errorf("readable SQL evidence has unsafe scenario %q", record.Scenario)
	}
	if record.Repetition < 1 || record.Index < 1 {
		return "", fmt.Errorf("readable SQL evidence identity is incomplete")
	}
	name := fmt.Sprintf("repetition-%02d-attempt-%02d.sql", record.Repetition, record.Index)
	return filepath.Join(root, "sql", string(record.Path), record.Scenario, name), nil
}

func ensureSQLFile(path, sql string) error {
	body := []byte(sql)
	if !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	if existing, err := os.ReadFile(path); err == nil {
		if !bytes.Equal(existing, body) {
			return fmt.Errorf("existing SQL evidence %q differs from attempt journal", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect SQL evidence %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create SQL evidence directory: %w", err)
	}
	return writeFile(path, func(w io.Writer) error {
		_, err := w.Write(body)
		return err
	})
}

func ensureSQLAttemptFiles(root string, records []s2sbench.AttemptRecord) error {
	for index, record := range records {
		path, err := sqlEvidencePath(root, record)
		if err != nil {
			return fmt.Errorf("derive readable SQL evidence for record %d: %w", index, err)
		}
		if path == "" {
			continue
		}
		if err := ensureSQLFile(path, record.SQL); err != nil {
			return fmt.Errorf("write readable SQL evidence for record %d: %w", index, err)
		}
	}
	return nil
}

func (j *runJournal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	var firstErr error
	if j.stream != nil {
		if err := j.stream.Close(); err != nil {
			firstErr = err
		}
		j.stream = nil
	}
	if j.audit != nil {
		if err := j.audit.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		j.audit = nil
	}
	return firstErr
}

func (j *runJournal) Commit(collection s2sbench.Collection, report s2sbench.V0Report) error {
	if j == nil {
		return fmt.Errorf("run journal is required")
	}
	if err := j.Close(); err != nil {
		return fmt.Errorf("close incremental attempt journal: %w", err)
	}
	if err := writeJSONFile(filepath.Join(j.partial, "raw-assets.json"), collection.RawAssets); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(j.partial, "metis.json"), collection.Metis); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(j.partial, "report.json"), report); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(j.partial, "summary.log"), func(w io.Writer) error {
		_, err := fmt.Fprintln(w, formatABSummaryAuditLog(report.Comparison))
		return err
	}); err != nil {
		return err
	}
	if err := publishDirectory(j.partial, j.output); err != nil {
		return fmt.Errorf("commit benchmark result bundle: %w", err)
	}
	return nil
}

func (j *runJournal) CommitArm(artifact s2sbench.ArmRunArtifact, report s2sbench.V0ArmReport) error {
	if j == nil {
		return fmt.Errorf("run journal is required")
	}
	if err := j.Close(); err != nil {
		return fmt.Errorf("close incremental attempt journal: %w", err)
	}
	name := ""
	switch artifact.Path {
	case s2sbench.PathRawAssets:
		name = "raw-assets.json"
	case s2sbench.PathMetis:
		name = "metis.json"
	default:
		return fmt.Errorf("unknown benchmark path %q", artifact.Path)
	}
	if err := writeJSONFile(filepath.Join(j.partial, name), artifact); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(j.partial, "arm-report.json"), report); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(j.partial, "summary.log"), func(w io.Writer) error {
		headline := headlineForArm(artifact.Path, report.Summary)
		_, err := fmt.Fprintf(w, "Summary=Arm|Path=%s|Total=%d|Correct=%d|Wrong=%d|Failed=%d|Accuracy=%.2f%%|FirstTryAccuracy=%.2f%%\n", headline.Path, headline.Total, headline.Correct, headline.Wrong, headline.Failed, headline.AccuracyPercent, headline.FirstTryAccuracyPercent)
		return err
	}); err != nil {
		return err
	}
	if err := publishDirectory(j.partial, j.output); err != nil {
		return fmt.Errorf("commit benchmark result bundle: %w", err)
	}
	return nil
}

func headlineForArm(path s2sbench.Path, report s2sbench.Report) s2sbench.ABArmSummary {
	headline := s2sbench.ABArmSummary{Path: path}
	for _, stratum := range s2sbench.Strata {
		entry := report.ByStratum[stratum]
		headline.Total += entry.Questions
		headline.Correct += entry.Correct
		headline.Wrong += entry.Wrong
		headline.Failed += entry.Failed
		headline.FirstTryCorrect += entry.FirstTryRight
	}
	if headline.Total > 0 {
		headline.AccuracyPercent = float64(headline.Correct) * 100 / float64(headline.Total)
		headline.FirstTryAccuracyPercent = float64(headline.FirstTryCorrect) * 100 / float64(headline.Total)
	}
	return headline
}
