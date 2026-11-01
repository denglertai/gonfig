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
	// YML represents a YML file
	YML FileType = "yml"
	// XML represents an XML file
	XML FileType = "xml"
	// PROPERTIES represents a properties file
	PROPERTIES FileType = "properties"
	// PLAIN represents a plain text file
	PLAIN FileType = "plain"
	// TOML represents a TOML file
	TOML FileType = "toml"
	// ENV represents a dotenv file, e.g. .env
	ENV FileType = "env"
	// DOTENV is another name of ENV for -t
	DOTENV FileType = "dotenv"
	// INI represents an INI file
	INI FileType = "ini"
)
