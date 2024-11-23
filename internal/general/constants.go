package general

// FileType represents the type of file to be read
type FileType string

const (
	// Undefined represents an undefined file type
	Undefined FileType = ""
	// JSON represents a JSON file
	JSON FileType = "json"
	// YAML represents a YAML file
	YAML FileType = "yaml"
	// XML represents an XML file
	XML FileType = "xml"
	// PROPERTIES represents a properties file
	PROPERTIES FileType = "properties"
)
