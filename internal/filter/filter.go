package filter

import (
	"os"
	"strings"
)

// Filter provides a basic abstraction for being able to process input and transform or validate it as needed
type Filter interface {
	// Process executes the filter's function
	Process(value string) (string, error)
}

// FilterParams makes a filter confiruable
type FilterParams interface {
	// AcceptParams allows providing additional settings to a filter
	AcceptParams(params map[string]string)
}

// DefaultFilterParamsHandler is a default implementation of FilterParams
type DefaultFilterParamsHandler struct {
	params map[string]string
}

// AcceptParams accepts the provided parameters
func (f *DefaultFilterParamsHandler) AcceptParams(params map[string]string) {
	f.params = params
}

// EnvVarFilter is a filter that replaces the value with the value of an environment variable
type EnvVarFilter struct {
	envVar string
}

// Process replaces the value with the value of an environment variable
func (f *EnvVarFilter) Process(_ string) (string, error) {
	value, found := os.LookupEnv(f.envVar)
	if !found {
		return "", nil
	}

	return value, nil
}

// NewEnvVarFilter creates a new EnvVarFilter
func NewEnvVarFilter(envVar string) *EnvVarFilter {
	return &EnvVarFilter{
		envVar: envVar,
	}
}

// FileInterceptorFilter is a filter that intercepts file references and reads the file content
type FileInterceptorFilter struct {
}

// Process reads the file content if the value is a file reference
func (f *FileInterceptorFilter) Process(value string) (string, error) {
	if strings.HasPrefix(value, "@") {
		path := value[1:]
		if _, err := os.Stat(path); err != nil {
			return value, nil
		}

		file, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}

		return string(file), nil
	}

	return value, nil
}

// NewFileInterceptorFilter creates a new FileInterceptorFilter
func NewFileInterceptorFilter() *FileInterceptorFilter {
	return &FileInterceptorFilter{}
}

// ApplyFilters applies a list of filters to a value
func ApplyFilters(value string, filters []Filter) (string, error) {
	for _, filter := range filters {
		var err error
		value, err = filter.Process(value)
		if err != nil {
			return "", err
		}
	}

	return value, nil
}

// notFoundFilter is a filter that is used when a filter is not found
type notFoundFilter struct {
	filter string
	DefaultFilterParamsHandler
}

// Process returns the filter name as the value
func (f notFoundFilter) Process(value string) (string, error) {
	return value, nil
}

// FilterMap is a map of filter names to filter constructors
var filterMap = map[string]func(string) Filter{}

// NewFilter creates a new filter based on the provided token
func NewFilter(token string) Filter {
	if constructor, found := filterMap[token]; found {
		return constructor(token)
	}

	return &notFoundFilter{
		filter: token,
	}
}
