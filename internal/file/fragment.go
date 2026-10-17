package file

import "github.com/denglertai/gonfig/internal/filter"

// FragmentConfigEntry is implemented by entries that can be replaced by or merged with a fragment,
// e.g. YAML and JSON values and keys or the content of XML elements
type FragmentConfigEntry interface {
	ConfigEntry
	// SetFragment parses the fragment in the format of the file and inserts it in place of the entry.
	// It returns the entries of the inserted fragment, so their placeholders can be processed as well.
	SetFragment(fragment filter.Fragment) ([]ConfigEntry, error)
}
