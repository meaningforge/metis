package command

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
)

type readinessJournal struct {
	output  string
	partial string
	stream  *incrementalJSONL[readiness.AttemptRecord]
}

func newReadinessJournal(output string, manifest readiness.Manifest, ledger readiness.Ledger) (*readinessJournal, error) {
	partial, stream, err := createIncrementalBundle[readiness.Manifest, readiness.AttemptRecord](output, manifest)
	if err != nil {
		return nil, err
	}
	if err := writeJSONFile(filepath.Join(partial, "fact-ledger.json"), ledger); err != nil {
		_ = stream.Close()
		_ = os.RemoveAll(partial)
		return nil, err
	}
	return &readinessJournal{output: output, partial: partial, stream: stream}, nil
}

func resumeReadinessJournal(output string, manifest readiness.Manifest, ledger readiness.Ledger) (*readinessJournal, []readiness.AttemptRecord, error) {
	partial, stream, records, err := resumeIncrementalBundle[readiness.Manifest, readiness.AttemptRecord](output, manifest)
	if err != nil {
		return nil, nil, err
	}
	persistedLedger, err := readStrictJSON[readiness.Ledger](filepath.Join(partial, "fact-ledger.json"))
	if err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("read persisted S2SBench fact ledger: %w", err)
	}
	if !reflect.DeepEqual(persistedLedger, ledger) {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("resume fact ledger differs from persisted frozen S2SBench evidence")
	}
	return &readinessJournal{output: output, partial: partial, stream: stream}, records, nil
}

func (j *readinessJournal) Append(record readiness.AttemptRecord) error {
	if j == nil || j.stream == nil {
		return fmt.Errorf("S2SBench journal is required")
	}
	return j.stream.Append(record)
}

func (j *readinessJournal) Close() error {
	if j == nil || j.stream == nil {
		return nil
	}
	err := j.stream.Close()
	j.stream = nil
	return err
}

func (j *readinessJournal) Commit(collection readiness.Collection, report readiness.Report) error {
	if j == nil {
		return fmt.Errorf("S2SBench journal is required")
	}
	if err := j.Close(); err != nil {
		return fmt.Errorf("close S2SBench attempt journal: %w", err)
	}
	if err := writeJSONFile(filepath.Join(j.partial, "collection.json"), collection); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(j.partial, "report.json"), report); err != nil {
		return err
	}
	if err := publishDirectory(j.partial, j.output); err != nil {
		return fmt.Errorf("commit readiness result bundle: %w", err)
	}
	return nil
}
