package file

import "github.com/denglertai/gonfig/internal/filter"

// FragmentConfigEntry is implemented by entries that can be replaced by or merged with a fragment,
// e.g. YAML and JSON values and keys or the content of XML elements
type FragmentConfigEntry interface {
	ConfigEntry
	// SetFragment parses fragment.Text in the format of the file and replaces the entry's content,
	// or merges it into the containing mapping or object for a key.
	// It returns the entries of the inserted fragment, so their placeholders can be processed as well.
	// It returns an error if parsing fails or the fragment is unsupported at the entry.
	SetFragment(fragment filter.Fragment) ([]ConfigEntry, error)
}
