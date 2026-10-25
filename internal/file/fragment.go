package file

import (
	"fmt"

	"github.com/denglertai/gonfig/internal/filter"
)

// FragmentConfigEntry is implemented by entries that can be replaced by or merged with a fragment,
// e.g. YAML and JSON values and keys, TOML values or the content of XML elements
type FragmentConfigEntry interface {
	ConfigEntry
	// SetFragment parses fragment.Text in the format of the file and replaces the entry's content,
	// or merges it into the containing mapping or object for a key.
	// It returns the entries of the inserted fragment, so their placeholders can be processed as well.
	// It returns an error if parsing fails or the fragment is unsupported at the entry.
	SetFragment(fragment filter.Fragment) ([]ConfigEntry, error)
}

// fragmentError returns the error of a fragment that cannot be parsed, e.g. "invalid YAML fragment on line 2".
// The messages of the parsers may quote parts of the fragment, e.g. an unknown YAML anchor or XML entity, so they are
// only included if values are allowed in errors, see filter.SetValuesInErrors. The location, e.g. "on line 2",
// never contains parts of the fragment.
func fragmentError(format string, location string, err error) error {
	if filter.ValuesInErrors() {
		return fmt.Errorf("invalid %s fragment: %w", format, err)
	}
	if location != "" {
		return fmt.Errorf("invalid %s fragment %s", format, location)
	}
	return fmt.Errorf("invalid %s fragment", format)
}
