// Package schema validates processed files against a JSON Schema, see Load and Schema.Validate
package schema

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/internal/general"
	"github.com/pelletier/go-toml/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"go.yaml.in/yaml/v3"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Schema is a compiled JSON Schema, which processed JSON, YAML and TOML files are validated against
type Schema struct {
	schema *jsonschema.Schema
	// name is the path of the schema file as given, which names it in messages
	name string
}

// Violation is a part of a document that does not match the schema
type Violation struct {
	// Path is the dot separated path of the value, like the paths of entries, or empty for the whole document.
	// Documents of YAML files with several documents are prefixed by their number, e.g. "document 2: a.b".
	Path string `json:"path"`
	// Message describes the violation without the value, unless values are allowed in errors, see filter.SetValuesInErrors
	Message string `json:"message"`
}

// String returns the violation as "path: message", with "(root)" as the path of the whole document
func (v Violation) String() string {
	path := v.Path
	if path == "" || strings.HasSuffix(path, ": ") {
		path += "(root)"
	}
	return path + ": " + v.Message
}

// Error is returned by Schema.Check for a file that does not match the schema
type Error struct {
	File       string
	Schema     string
	Violations []Violation
}

// Error lists the violations, one per line
func (e *Error) Error() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s does not match the schema %s:", e.File, e.Schema)
	for _, violation := range e.Violations {
		sb.WriteString("\n  - ")
		sb.WriteString(violation.String())
	}
	return sb.String()
}

// Load reads and compiles the JSON Schema file. References to other schemas are resolved from local files only,
// besides the meta-schemas of json-schema.org, which are built in, so loading a schema never accesses the network.
// The format keyword is asserted, e.g. "format": "email" rejects strings that are no email address.
func Load(path string) (*Schema, error) {
	// The schema is read here, so errors name the file as given instead of its URL
	data, err := os.ReadFile(path) //nolint:gosec // the path is the schema the user asked for
	if err != nil {
		return nil, fmt.Errorf("failed to load the schema: %w", err)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to load the schema %s: invalid JSON: %w", path, err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	// References relative to the schema are resolved from its directory, since it is added under its own path.
	// Adding fails only for a URL added before or of a meta-schema, which the path of a new compiler and a file is not.
	_ = compiler.AddResource(path, document)
	compiled, err := compiler.Compile(path)
	if err != nil {
		return nil, fmt.Errorf("invalid schema %s: %w", path, err)
	}
	return &Schema{schema: compiled, name: path}, nil
}

// Check validates the processed content of the file and returns an *Error listing the violations if it does not match
func (s *Schema) Check(file string, fileType general.FileType, data []byte) error {
	violations, err := s.Validate(fileType, data)
	if err != nil {
		return fmt.Errorf("failed to validate %s against the schema %s: %w", file, s.name, err)
	}
	if len(violations) > 0 {
		return &Error{File: file, Schema: s.name, Violations: violations}
	}
	return nil
}

// Validate validates the processed content of a file of the type and returns the violations, ordered by their path.
// Every document of a YAML file is validated. It returns an error if the type is not supported or the content
// cannot be decoded.
func (s *Schema) Validate(fileType general.FileType, data []byte) ([]Violation, error) {
	documents, err := decode(fileType, data)
	if err != nil {
		return nil, err
	}

	var violations []Violation
	for i, document := range documents {
		prefix := ""
		if len(documents) > 1 {
			prefix = fmt.Sprintf("document %d: ", i+1)
		}

		// The validator returns a *ValidationError for every violation, including values of types it doesn't support
		var validationError *jsonschema.ValidationError
		if errors.As(s.schema.Validate(document), &validationError) {
			violations = appendViolations(violations, validationError, prefix)
		}
	}
	return violations, nil
}

// appendViolations appends the causes of the validation error without further causes, which are the actual violations.
// The validator collects the causes of objects in the order of a map, so they are sorted to report them consistently.
func appendViolations(violations []Violation, validationError *jsonschema.ValidationError, prefix string) []Violation {
	start := len(violations)
	var collect func(e *jsonschema.ValidationError)
	collect = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			violations = append(violations, Violation{Path: prefix + strings.Join(e.InstanceLocation, "."), Message: describe(e.ErrorKind)})
			return
		}
		for _, cause := range e.Causes {
			collect(cause)
		}
	}
	collect(validationError)

	slices.SortStableFunc(violations[start:], func(a, b Violation) int {
		return cmp.Or(cmp.Compare(a.Path, b.Path), cmp.Compare(a.Message, b.Message))
	})
	return violations
}

// printer formats the messages of the validator in English
var printer = message.NewPrinter(language.English)

// describe returns the message of the violation. The messages of the validator include the value for some keywords,
// e.g. "'s3cret' does not match pattern '^[a-z]{12}$'", and values may be secrets. Unless values are allowed in errors,
// these keywords get messages naming the requirement only. Numbers get own messages in any case, since the validator
// formats them with digit grouping, e.g. 65,535.
func describe(errorKind jsonschema.ErrorKind) string {
	var requirement string
	var got *big.Rat
	switch k := errorKind.(type) {
	case *kind.Pattern:
		requirement = fmt.Sprintf("value does not match pattern '%s'", k.Want)
	case *kind.Format:
		requirement = fmt.Sprintf("value is not valid %s", k.Want)
	case *kind.Minimum:
		requirement, got = "value must be >= "+number(k.Want), k.Got
	case *kind.Maximum:
		requirement, got = "value must be <= "+number(k.Want), k.Got
	case *kind.ExclusiveMinimum:
		requirement, got = "value must be > "+number(k.Want), k.Got
	case *kind.ExclusiveMaximum:
		requirement, got = "value must be < "+number(k.Want), k.Got
	case *kind.MultipleOf:
		requirement, got = "value must be a multiple of "+number(k.Want), k.Got
	default:
		return errorKind.LocalizedString(printer)
	}

	switch {
	case !filter.ValuesInErrors():
		return requirement
	case got != nil:
		return requirement + ", got " + number(got)
	default:
		return errorKind.LocalizedString(printer)
	}
}

// number formats a number of the validator like in JSON, e.g. 1.5 instead of 3/2
func number(n *big.Rat) string {
	if n.IsInt() {
		return n.RatString()
	}
	f, _ := n.Float64()
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// decode decodes the documents of the content into the values the validator expects: maps with string keys, slices,
// strings, numbers, booleans and nil. The BOM is removed, which processed files keep if their input had one.
func decode(fileType general.FileType, data []byte) ([]any, error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	switch fileType {
	case general.JSON:
		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return []any{document}, nil
	case general.YAML, general.YML:
		var documents []any
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		for {
			var document any
			err := decoder.Decode(&document)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			documents = append(documents, normalize(document))
		}
		// An empty file is a single empty document, which is null
		if len(documents) == 0 {
			documents = []any{nil}
		}
		return documents, nil
	case general.TOML:
		var document map[string]any
		if err := toml.Unmarshal(data, &document); err != nil {
			return nil, err
		}
		return []any{normalize(document)}, nil
	default:
		return nil, fmt.Errorf("files of type %s cannot be validated, only JSON, YAML and TOML files", fileType)
	}
}

// normalize converts the decoded values the validator does not support: mappings with keys other than strings,
// which YAML allows, and the date and time values of TOML, which are converted to their text as in the file
func normalize(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = normalize(item)
		}
		return v
	case map[any]any:
		converted := make(map[string]any, len(v))
		for key, item := range v {
			converted[fmt.Sprint(key)] = normalize(item)
		}
		return converted
	case []any:
		for i, item := range v {
			v[i] = normalize(item)
		}
		return v
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case toml.LocalDate, toml.LocalTime, toml.LocalDateTime:
		return fmt.Sprint(v)
	default:
		return v
	}
}
