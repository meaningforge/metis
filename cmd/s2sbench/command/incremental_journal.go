package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	benchartifact "github.com/meaningforge/metis/cmd/s2sbench/bench/artifact"
)

// incrementalJSONL is the shared crash-safe evidence stream used by live
// S2SBench experiments. Experiment-specific journals own their sidecars and
// final report shapes, while creation, strict resume, fsync, and close semantics
// remain identical.
type incrementalJSONL[Record any] struct {
	mu     sync.Mutex
	file   *os.File
	closed bool
}

func createIncrementalBundle[Manifest, Record any](output string, manifest Manifest) (string, *incrementalJSONL[Record], error) {
	parent := filepath.Dir(output)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", nil, fmt.Errorf("create result parent directory: %w", err)
	}
	partial := output + ".partial"
	if err := os.Mkdir(partial, 0o700); err != nil {
		if os.IsExist(err) {
			return "", nil, fmt.Errorf("partial result directory %q already exists; use --resume or move it before retrying", partial)
		}
		return "", nil, fmt.Errorf("create partial result directory %q: %w", partial, err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(partial)
		}
	}()
	if err := writeJSONFile(filepath.Join(partial, "manifest.json"), manifest); err != nil {
		return "", nil, err
	}
	file, err := os.OpenFile(filepath.Join(partial, "attempts.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", nil, fmt.Errorf("create incremental attempt journal: %w", err)
	}
	cleanup = false
	return partial, &incrementalJSONL[Record]{file: file}, nil
}

func resumeIncrementalBundle[Manifest, Record any](output string, manifest Manifest) (string, *incrementalJSONL[Record], []Record, error) {
	partial := output + ".partial"
	persisted, err := readStrictJSON[Manifest](filepath.Join(partial, "manifest.json"))
	if err != nil {
		return "", nil, nil, fmt.Errorf("read persisted manifest: %w", err)
	}
	if !reflect.DeepEqual(persisted, manifest) {
		return "", nil, nil, fmt.Errorf("resume manifest differs from persisted frozen run identity")
	}
	records, err := readStrictJSONL[Record](filepath.Join(partial, "attempts.jsonl"))
	if err != nil {
		return "", nil, nil, fmt.Errorf("read incremental attempt journal: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(partial, "attempts.jsonl"), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", nil, nil, fmt.Errorf("open incremental attempt journal for resume: %w", err)
	}
	return partial, &incrementalJSONL[Record]{file: file}, records, nil
}

func (j *incrementalJSONL[Record]) Append(record Record) error {
	if j == nil {
		return fmt.Errorf("incremental attempt journal is required")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed || j.file == nil {
		return fmt.Errorf("incremental attempt journal is closed")
	}
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("append incremental attempt evidence: %w", err)
	}
	body = append([]byte(benchartifact.RedactString(string(body))), '\n')
	if _, err := j.file.Write(body); err != nil {
		return fmt.Errorf("append incremental attempt evidence: %w", err)
	}
	if err := j.file.Sync(); err != nil {
		return fmt.Errorf("sync incremental attempt evidence: %w", err)
	}
	return nil
}

func (j *incrementalJSONL[Record]) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

func readStrictJSON[Value any](path string) (Value, error) {
	var value Value
	file, err := os.Open(path)
	if err != nil {
		return value, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return value, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return value, fmt.Errorf("file contains more than one JSON value")
		}
		return value, fmt.Errorf("decode trailing data: %w", err)
	}
	return value, nil
}

func readStrictJSONL[Record any](path string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var records []Record
	for {
		var record Record
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return records, nil
			}
			return nil, fmt.Errorf("decode record %d: %w", len(records), err)
		}
		records = append(records, record)
	}
}
