package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/meaningforge/metis/cmd/s2sbench/bench/runner/readiness"
	"github.com/spf13/cobra"
)

func newAnalyzeCommand(stdout io.Writer) *cobra.Command {
	return newAnalysisCommand("analyze", stdout)
}

func newReportCommand(stdout io.Writer) *cobra.Command {
	return newAnalysisCommand("report", stdout)
}

func newAnalysisCommand(use string, stdout io.Writer) *cobra.Command {
	var outputPath string
	var inputs []string
	command := &cobra.Command{Use: use, Short: "Compare OKF and Metis MCP single-arm results", Args: cobra.NoArgs}
	command.Flags().StringVar(&outputPath, "output", "", "comparison report JSON path; stdout when omitted")
	command.Flags().StringArrayVar(&inputs, "input", nil, "single-arm result directory or collection.json; repeatable")
	command.RunE = func(*cobra.Command, []string) error { return analyze(inputs, outputPath, stdout) }
	return command
}

func runAnalyze(args []string, stdout io.Writer) error {
	command := newAnalyzeCommand(stdout)
	command.SetArgs(args)
	command.SilenceErrors = true
	command.SilenceUsage = true
	return command.Execute()
}

func analyze(inputs []string, outputPath string, stdout io.Writer) error {
	if len(inputs) != 2 {
		return fmt.Errorf("analyze requires exactly two --input values: OKF and Metis MCP")
	}
	collections := make([]readiness.Collection, 0, len(inputs))
	for index, input := range inputs {
		collection, err := readReadinessCollection(input)
		if err != nil {
			return fmt.Errorf("read input %d: %w", index+1, err)
		}
		collections = append(collections, collection)
	}
	report, err := readiness.BuildComparisonReport(collections...)
	if err != nil {
		return err
	}
	if outputPath == "" {
		return writeJSON(stdout, report)
	}
	if _, err := os.Stat(outputPath); err == nil {
		return fmt.Errorf("output file %q already exists; refusing to overwrite comparison evidence", outputPath)
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeJSONFile(outputPath, report)
}

func readReadinessCollection(path string) (readiness.Collection, error) {
	info, err := os.Stat(path)
	if err != nil {
		return readiness.Collection{}, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "collection.json")
	}
	file, err := os.Open(path)
	if err != nil {
		return readiness.Collection{}, err
	}
	defer file.Close()
	var collection readiness.Collection
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&collection); err != nil {
		return readiness.Collection{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return readiness.Collection{}, fmt.Errorf("collection contains more than one JSON value")
		}
		return readiness.Collection{}, fmt.Errorf("decode trailing collection data: %w", err)
	}
	return collection, nil
}
