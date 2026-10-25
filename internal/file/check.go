package file

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/denglertai/gonfig/internal/diff"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/denglertai/gonfig/internal/value"
)

// Statuses of environment variables in a CheckReport
const (
	VariableSet     = "set"
	VariableEmpty   = "empty"
	VariableMissing = "missing"
)

// CheckReport is the result of checking a file: every entry containing placeholders with the environment variables
// and templates it uses and the error processing it would cause. It never contains values, since they may be secrets,
// unless values may be logged and the diff is requested.
type CheckReport struct {
	File    string        `json:"file"`
	Entries []EntryReport `json:"entries"`
	// Error is the reason the file cannot be read, e.g. because it does not exist or is invalid, so it has no entries
	Error string `json:"error,omitempty"`
	// Diff is the unified diff of the changes processing the file would make, with masked values unless values may be
	// logged. It is only set if requested by FileProcessor.Diff, and empty if nothing would change.
	Diff *string `json:"diff,omitempty"`
	// SchemaViolations are the parts of the processed file that do not match the schema given by --schema
	SchemaViolations []schema.Violation `json:"schemaViolations,omitempty"`
}

// EntryReport describes an entry containing placeholders
type EntryReport struct {
	Path      string           `json:"path"`
	Variables []VariableReport `json:"variables"`
	Templates []string         `json:"templates,omitempty"`
	Error     string           `json:"error,omitempty"`
}

// VariableReport describes an environment variable read by the placeholders of an entry
type VariableReport struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	// HasDefault marks variables for which every placeholder reading it provides a default
	HasDefault bool `json:"hasDefault"`
}

// Missing returns whether the variable is not set and has no default, which fails the check
func (v VariableReport) Missing() bool {
	return v.Status == VariableMissing && !v.HasDefault
}

// addVariable adds the lookup of a variable. A variable read several times has a default only if every lookup provides one.
func (e *EntryReport) addVariable(lookup filter.VariableLookup) {
	status := VariableSet
	switch {
	case !lookup.Set:
		status = VariableMissing
	case lookup.Empty:
		status = VariableEmpty
	}

	for i := range e.Variables {
		if e.Variables[i].Name == lookup.Name {
			e.Variables[i].HasDefault = e.Variables[i].HasDefault && lookup.HasDefault
			return
		}
	}
	e.Variables = append(e.Variables, VariableReport{Name: lookup.Name, Status: status, HasDefault: lookup.HasDefault})
}

// Errors returns the number of entries that cannot be processed and of schema violations, plus one if the file cannot be read
func (r *CheckReport) Errors() int {
	count := len(r.SchemaViolations)
	if r.Error != "" {
		count++
	}
	for _, entry := range r.Entries {
		if entry.Error != "" {
			count++
		}
	}
	return count
}

// MissingVariables returns the number of variables that are not set and have no default, counted once per entry
func (r *CheckReport) MissingVariables() int {
	count := 0
	for _, entry := range r.Entries {
		for _, variable := range entry.Variables {
			if variable.Missing() {
				count++
			}
		}
	}
	return count
}

// Failed returns whether processing the file would fail or use variables that are not set and have no default
func (r *CheckReport) Failed() bool {
	return r.Errors() > 0 || r.MissingVariables() > 0
}

// Check processes the file like Process, but writes nothing and collects all problems instead of stopping at the first one.
// It returns an error only if the file cannot be read.
func (fp *FileProcessor) Check() (*CheckReport, error) {
	handler, err := fp.getFileProcessor()
	if err != nil {
		return nil, err
	}
	return fp.checkWith(handler)
}

// checkWith checks the file with the given handler, see Check
func (fp *FileProcessor) checkWith(handler ConfigFileHandler) (*CheckReport, error) {
	handler, entries, err := fp.readWith(handler)
	if err != nil {
		return nil, err
	}

	fp.report = &CheckReport{File: fp.FileName, Entries: []EntryReport{}}
	defer func() { fp.report = nil }()

	for entry := range entries {
		// Errors are recorded in the report
		_ = fp.processEntry(entry, true, fp.fileGroup())
	}

	if fp.Diff {
		// Entries with errors keep their placeholders, so the diff shows the changes of all other entries
		var processed bytes.Buffer
		if err := fp.write(handler, &processed); err != nil {
			fp.report.Error = fmt.Sprintf("failed to write %s: %v", fp.FileName, err)
			return fp.report, nil
		}
		changes := diff.Unified(fp.FileName, fp.FileName, string(fp.source), processed.String())
		fp.report.Diff = &changes
	}

	return fp.report, nil
}

// checkEntry processes the entry like applyEntry and records the variables, templates and error in the report
func (fp *FileProcessor) checkEntry(entry ConfigEntry, current string, allowFragments bool) error {
	// Entries with escape sequences like "$${" only are not reported, but unescaped, so the diff shows the change
	if !value.HasPlaceholders(current) {
		return fp.applyEntry(entry, current, allowFragments, fp.fileGroup())
	}

	// Reserve the position of the entry, since processing a fragment adds the entries of the fragment
	index := len(fp.report.Entries)
	fp.report.Entries = append(fp.report.Entries, EntryReport{})

	report := EntryReport{Path: entry.Path(), Variables: []VariableReport{}, Templates: value.TemplateReferences(current)}
	filter.SetLookupRecorder(report.addVariable)
	err := fp.applyEntry(entry, current, allowFragments, fp.fileGroup())
	filter.SetLookupRecorder(nil)

	if err != nil {
		// The path and file name are part of the report already, so only the reason is recorded
		if reason := errors.Unwrap(err); reason != nil {
			err = reason
		}
		report.Error = err.Error()
	}

	fp.report.Entries[index] = report
	return nil
}
