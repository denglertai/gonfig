package filter

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/denglertai/gonfig/pkg/logging"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptFilterKey       = "bcrypt"
	md5FilterKey          = "md5"
	base64FilterKey       = "base64"
	base64DecodeFilterKey = "base64_decode"
)

// Filter provides a basic abstraction for being able to process input and transform or validate it as needed
type Filter interface {
	// Process executes the filter's function
	Process(value any) (any, error)
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
func (f *EnvVarFilter) Process(_ any) (any, error) {
	logging.Debug("Processing EnvVarFilter", "envVar", f.envVar)

	value, found := os.LookupEnv(f.envVar)
	if !found {
		// if the environment variable is not found, return an empty string and continue
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
func (f *FileInterceptorFilter) Process(value any) (any, error) {
	s := value.(string)

	if strings.HasPrefix(s, "@") {
		path := s[1:]
		logging.Debug("Processing FileInterceptorFilter", "path", path)
		if _, err := os.Stat(path); err != nil {
			// if the file does not exist, return an empty string and continue
			return value, nil
		}

		file, err := os.ReadFile(path)
		if err != nil {
			// if the file exists but cannot be read, return and empty string and the error as is
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
func ApplyFilters(value any, filters []Filter) (any, error) {
	logging.Debug("Applying filters", "filters", len(filters))
	for _, filter := range filters {
		var err error
		value, err = filter.Process(value)
		if err != nil {
			return "", err
		}
	}
	logging.Debug("Finished applying filters")

	return value, nil
}

// notFoundFilter is a filter that is used when a filter is not found
type notFoundFilter struct {
	filter string
	DefaultFilterParamsHandler
}

// Process returns the filter name as the value
func (f notFoundFilter) Process(value any) (any, error) {
	logging.Warn("Filter not found, returning original value", "filter", f.filter)
	return value, nil
}

// FilterMap is a map of filter names to filter constructors
var filterMap = map[string]func(string) Filter{}

// NewFilter creates a new filter based on the provided token
func NewFilter(token string) Filter {
	logging.Debug("Creating filter", "token", token)

	if constructor, found := filterMap[token]; found {
		return constructor(token)
	}

	return &notFoundFilter{
		filter: token,
	}
}

// FuncFilter is a filter that uses a function to process the value
type FuncFilter struct {
	fn func(any, map[string]string) (any, error)
	DefaultFilterParamsHandler
}

// Process executes the filter's function
func (f *FuncFilter) Process(value any) (any, error) {
	return f.fn(value, f.DefaultFilterParamsHandler.params)
}

// init initializes the filter map
func init() {
	filterMap["upper"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return strings.ToUpper(value.(string)), nil
			},
		}
	}
	filterMap["lower"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return strings.ToLower(value.(string)), nil
			},
		}
	}
	filterMap["trim"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return strings.TrimSpace(value.(string)), nil
			},
		}
	}
	filterMap["trimleft"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return strings.TrimLeft(value.(string), " "), nil
			},
		}
	}
	filterMap["trimright"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return strings.TrimRight(value.(string), " "), nil
			},
		}
	}

	filterMap["to_int"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return strconv.Atoi(value.(string))
			},
		}
	}

	filterMap["multiply"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				multiplier, found := params["m"]
				if !found {
					multiplier = "1"
				}

				m, err := strconv.Atoi(multiplier)
				if err != nil {
					return "", err
				}

				return value.(int) * m, nil
			},
		}
	}

	filterMap[bcryptFilterKey] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				hash, err := bcrypt.GenerateFromPassword([]byte(value.(string)), bcrypt.DefaultCost)
				if err != nil {
					return "", err
				}

				return string(hash), nil
			},
		}
	}

	filterMap[md5FilterKey] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				hash := md5.New()
				_, err := hash.Write([]byte(value.(string)))
				if err != nil {
					return "", err
				}
				// return the md5 hash as a string
				return hex.EncodeToString(hash.Sum(nil)), nil
			},
		}
	}

	filterMap["base64"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return base64.StdEncoding.EncodeToString([]byte(value.(string))), nil
			},
		}
	}

	filterMap["base64_decode"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				decoded, err := base64.StdEncoding.DecodeString(value.(string))
				if err != nil {
					return "", err
				}
				return string(decoded), nil
			},
		}
	}

	filterMap["replace"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				oldStr, ok1 := params["old"]
				newStr, ok2 := params["new"]
				if !ok1 || !ok2 {
					return "", fmt.Errorf("replace filter requires 'old' and 'new' parameters")
				}
				return strings.ReplaceAll(value.(string), oldStr, newStr), nil
			},
		}
	}

	filterMap["regex"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				pattern, ok1 := params["pattern"]
				replacement, ok2 := params["replacement"]
				if !ok1 || !ok2 {
					return "", fmt.Errorf("regex filter requires 'pattern' and 'replacement' parameters")
				}

				re, err := regexp.Compile(pattern)
				if err != nil {
					return "", err
				}

				return re.ReplaceAllString(value.(string), replacement), nil
			},
		}
	}

	filterMap["split"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				delimiter, ok := params["delimiter"]
				if !ok {
					return "", fmt.Errorf("split filter requires 'delimiter' parameter")
				}
				return strings.Split(value.(string), delimiter), nil
			},
		}
	}

	filterMap["join"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				delimiter, ok := params["delimiter"]
				if !ok {
					return "", fmt.Errorf("join filter requires 'delimiter' parameter")
				}
				parts, ok := value.([]string)
				if !ok {
					return "", fmt.Errorf("join filter expects a slice of strings")
				}
				return strings.Join(parts, delimiter), nil
			},
		}
	}
}

// AddPluginFilters adds filters from a plugin
func AddPluginFilters(filters map[string]interface{}) {
	for name, filter := range filters {
		// Skip filters that are not of type Filter or already in the filter map
		if _, ok := filter.(Filter); !ok || filterMap[name] != nil {
			continue
		}

		filterMap[name] = func(token string) Filter {
			return filter.(Filter)
		}
	}
}
