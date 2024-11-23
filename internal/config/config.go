package config

import (
	"github.com/denglertai/gonfig/internal/general"
)

// Settings represents the configuration settings
type Settings struct {
	// File is the path to the configuration file
	File string

	// FileType is the type of file to be read. If not set, the file type will be inferred from the file extension
	FileType general.FileType
}

// NewSettings returns a new Settings instance
func NewSettings() *Settings {
	return &Settings{
		FileType: general.Undefined,
	}
}
