/*
Copyright © 2024 Daniel Englert
*/
package cmd

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/denglertai/gonfig/internal/general"
	"github.com/denglertai/gonfig/internal/schema"
	"github.com/denglertai/gonfig/pkg/logging"
	"github.com/spf13/cobra"
)

// schemaRule assigns a schema to the files matching its pattern
type schemaRule struct {
	// pattern is a glob pattern matching the base name of a file if it has no path separator, otherwise the absolute
	// path of a file. Empty matches every file, including stdin.
	pattern string
	// path is the path of the schema file
	path string
	// source names where the rule is defined in messages, e.g. --schema
	source string
}

// matches returns whether the rule applies to the file, given as passed to -f
func (r schemaRule) matches(name string) bool {
	if r.pattern == "" {
		return true
	}
	// stdin has no name a pattern could match
	if name == stdinFile {
		return false
	}
	if !hasSeparator(r.pattern) {
		matched, _ := filepath.Match(r.pattern, filepath.Base(name))
		return matched
	}
	// Abs only fails if the working directory is unknown, comparing the path as given then
	if abs, err := filepath.Abs(name); err == nil {
		name = abs
	}
	// The pattern has been checked when it was parsed, so matching cannot fail
	matched, _ := filepath.Match(r.pattern, name)
	return matched
}

// hasSeparator returns whether the pattern contains a path separator, so it matches paths instead of base names
func hasSeparator(pattern string) bool {
	return strings.ContainsAny(pattern, "/"+string(filepath.Separator))
}

// fileSchemas are the schemas each file of a run is validated against, by the file as passed to -f
type fileSchemas map[string][]*schema.Schema

// any returns whether any file is validated
func (s fileSchemas) any() bool {
	return len(s) > 0
}

// parseSchemaFlag parses a --schema value: the path of a schema for every file, or pattern=path for the files matching
// the pattern. Relative patterns with a directory are relative to the working directory.
func parseSchemaFlag(value string) (schemaRule, error) {
	pattern, path, mapped := strings.Cut(value, "=")
	if !mapped {
		return schemaRule{path: value, source: "--schema"}, nil
	}
	if pattern == "" || path == "" {
		return schemaRule{}, fmt.Errorf("invalid --schema %q, use <schema> or <pattern>=<schema>", value)
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return schemaRule{}, fmt.Errorf("invalid pattern %q of --schema %q: %w", pattern, value, err)
	}
	if hasSeparator(pattern) && !filepath.IsAbs(pattern) {
		// Abs only fails if the working directory is unknown, matching the paths as given then
		if abs, err := filepath.Abs(pattern); err == nil {
			pattern = abs
		}
	}
	return schemaRule{pattern: pattern, path: path, source: fmt.Sprintf("--schema %s", value)}, nil
}

// schemaRules returns the rules of the --schema flags or, if none are given, of the schemas in the config file
func schemaRules(cmd *cobra.Command) ([]schemaRule, error) {
	// The flag is defined for every command calling this, so reading it cannot fail
	values, _ := cmd.Flags().GetStringArray("schema")
	rules := make([]schemaRule, 0, len(values))
	for _, value := range values {
		rule, err := parseSchemaFlag(value)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	if len(values) > 0 {
		return rules, nil
	}

	// The config file resolved the paths already and checked the patterns
	cfg := GetAppConfig(cmd)
	for _, mapping := range cfg.Schemas {
		rules = append(rules, schemaRule{pattern: mapping.Files, path: mapping.Schema, source: "the schemas of " + cfg.ConfigFile})
	}
	return rules, nil
}

// loadSchemas loads the schemas each file is validated against: the schemas of every rule whose pattern it matches.
// --schema flags replace the schemas of the config file. Files matching no rule are not validated. Schemas are only
// loaded if a file matches them, so a config file may name schemas of files other runs process. XML, properties and
// plain text files have no structure a JSON Schema could describe, so they are rejected before any file is processed
// if a rule matches them. Files of unknown types are left to processing, which reports them.
func loadSchemas(cmd *cobra.Command, files []string, fileType general.FileType) (fileSchemas, error) {
	rules, err := schemaRules(cmd)
	if err != nil {
		return nil, err
	}

	loaded := map[string]*schema.Schema{}
	result := fileSchemas{}
	for _, rule := range rules {
		matched := false
		for _, name := range files {
			if !rule.matches(name) {
				continue
			}
			matched = true

			processor := newFileProcessor(cmd, name, fileType, io.Discard)
			switch processor.FileType {
			case general.XML, general.PROPERTIES, general.PLAIN:
				return nil, fmt.Errorf("%s validates JSON, YAML and TOML files only, not %s of type %s", rule.source, processor.FileName, processor.FileType)
			}

			sch, ok := loaded[rule.path]
			if !ok {
				if sch, err = schema.Load(rule.path); err != nil {
					return nil, err
				}
				loaded[rule.path] = sch
			}
			// A file matching several rules of the same schema is validated against it once
			if !slices.Contains(result[name], sch) {
				result[name] = append(result[name], sch)
			}
		}
		// A pattern of the command line is meant for the files of this run, so a typo is worth a warning
		if !matched && strings.HasPrefix(rule.source, "--schema") {
			logging.Warn("The pattern of --schema matches no file, so no file is validated against its schema", "schema", rule.source)
		}
	}
	return result, nil
}
