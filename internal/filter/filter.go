package filter

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/denglertai/gonfig/pkg/logging"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptFilterKey = "bcrypt"
	md5FilterKey    = "md5"

	// defaultListDelimiter is used by split and join if no delimiter is given
	defaultListDelimiter = ","
)

// backReferenceRe matches back references like \1 within regex replacements
var backReferenceRe = regexp.MustCompile(`\\(\d+)`)

// Filter provides a basic abstraction for being able to process input and transform or validate it as needed
type Filter interface {
	// Process executes the filter's function
	Process(value any) (any, error)
}

// FilterParams makes a filter configurable
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
	s, ok := value.(string)

	if ok && strings.HasPrefix(s, "@") {
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

// toString converts a filter input to a string, formatting non-string values
func toString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

// toInt converts a filter input to an int, parsing string values
func toInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case string:
		return strconv.Atoi(v)
	default:
		return 0, fmt.Errorf("cannot convert %v (%T) to int", value, value)
	}
}

// param returns the filter param given either by name or by position
func param(params map[string]string, name string, position int) (string, bool) {
	if value, found := params[name]; found {
		return value, true
	}
	value, found := params[strconv.Itoa(position)]
	return value, found
}

// requiredParam returns the filter param given either by name or by position or an error if it is missing
func requiredParam(filter string, params map[string]string, name string, position int) (string, error) {
	value, found := param(params, name, position)
	if !found {
		return "", fmt.Errorf("filter %s requires the param %s", filter, name)
	}
	return value, nil
}

// stringFunc creates a filter function applying fn to the string value, or to each element of a list created by split
func stringFunc(fn func(string, map[string]string) (string, error)) func(any, map[string]string) (any, error) {
	return func(value any, params map[string]string) (any, error) {
		list, ok := value.([]string)
		if !ok {
			return fn(toString(value), params)
		}

		result := make([]string, len(list))
		for i, s := range list {
			var err error
			result[i], err = fn(s, params)
			if err != nil {
				return "", err
			}
		}
		return result, nil
	}
}

// ApplyFilters applies a chain of filters to a value.
//
// For a placeholder like "${ENV_VAR | trim | upper}" the chain consists of:
//  1. EnvVarFilter, which ignores its input and returns the value of ENV_VAR
//  2. FileInterceptorFilter, which replaces "@path" by the content of the file at path
//  3. the filters of the placeholder in the order they are written, here trim and upper
//
// Each filter receives the output of the previous one. The values are usually strings,
// but filters may return other types like int (to_int) or []string (split).
// The first error stops the chain and is returned.
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

// allowUnknownFilters makes unknown filters pass values on unchanged instead of failing, see SetAllowUnknownFilters
var allowUnknownFilters bool

// SetAllowUnknownFilters sets whether unknown filters pass values on unchanged with a warning instead of failing.
// This may write a secret that was meant to be hashed or encrypted by a filter that is unknown, e.g. because of a typo.
func SetAllowUnknownFilters(allow bool) {
	allowUnknownFilters = allow
}

// notFoundFilter is used for unknown filter names, e.g. typos or filters of plugins that failed to load
type notFoundFilter struct {
	filter string
	DefaultFilterParamsHandler
}

// Process fails, since passing the value on unchanged could write a secret that was meant to be hashed or encrypted.
// If unknown filters are allowed, it logs a warning and returns the value unchanged instead.
func (f notFoundFilter) Process(value any) (any, error) {
	if allowUnknownFilters {
		logging.Warn("Unknown filter, passing the value on unchanged", "filter", f.filter)
		return value, nil
	}

	return "", fmt.Errorf("unknown filter %q: check the filter name, or the log for plugins that failed to load", f.filter)
}

// filterMap is a map of filter names to filter constructors.
// It contains the built-in filters registered by init and the filters added by plugins.
var filterMap = map[string]func(string) Filter{}

// NewFilter creates a new filter based on the provided token.
// Every usage of a filter gets its own instance, so params accepted by one usage do not affect others.
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
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				return strings.ToUpper(s), nil
			}),
		}
	}
	filterMap["lower"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				return strings.ToLower(s), nil
			}),
		}
	}
	filterMap["trim"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				return strings.TrimSpace(s), nil
			}),
		}
	}
	filterMap["trimleft"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				return strings.TrimLeft(s, " "), nil
			}),
		}
	}
	filterMap["trimright"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				return strings.TrimRight(s, " "), nil
			}),
		}
	}

	filterMap["to_int"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return toInt(value)
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

				v, err := toInt(value)
				if err != nil {
					return "", err
				}

				return v * m, nil
			},
		}
	}

	filterMap[bcryptFilterKey] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				hash, err := bcrypt.GenerateFromPassword([]byte(toString(value)), bcrypt.DefaultCost)
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
				_, err := hash.Write([]byte(toString(value)))
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
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				return base64.StdEncoding.EncodeToString([]byte(s)), nil
			}),
		}
	}

	filterMap["base64_decode"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, _ map[string]string) (string, error) {
				decoded, err := base64.StdEncoding.DecodeString(s)
				if err != nil {
					return "", fmt.Errorf("filter base64_decode: %w", err)
				}
				return string(decoded), nil
			}),
		}
	}

	filterMap["replace"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, params map[string]string) (string, error) {
				oldStr, err := requiredParam(token, params, "old", 0)
				if err != nil {
					return "", err
				}
				newStr, err := requiredParam(token, params, "new", 1)
				if err != nil {
					return "", err
				}
				return strings.ReplaceAll(s, oldStr, newStr), nil
			}),
		}
	}

	filterMap["regex"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, params map[string]string) (string, error) {
				pattern, err := requiredParam(token, params, "pattern", 0)
				if err != nil {
					return "", err
				}
				replacement, err := requiredParam(token, params, "replacement", 1)
				if err != nil {
					return "", err
				}

				re, err := regexp.Compile(pattern)
				if err != nil {
					return "", fmt.Errorf("filter regex: invalid pattern %q: %w", pattern, err)
				}

				// Support \1 style back references in addition to Go's ${1}
				replacement = backReferenceRe.ReplaceAllString(replacement, "$${$1}")
				return re.ReplaceAllString(s, replacement), nil
			}),
		}
	}

	filterMap["split"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				delimiter, found := param(params, "delimiter", 0)
				if !found {
					delimiter = defaultListDelimiter
				}
				return strings.Split(toString(value), delimiter), nil
			},
		}
	}

	filterMap["join"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				delimiter, found := param(params, "delimiter", 0)
				if !found {
					delimiter = defaultListDelimiter
				}
				list, ok := value.([]string)
				if !ok {
					// a single value is a list with one element
					return toString(value), nil
				}
				return strings.Join(list, delimiter), nil
			},
		}
	}
}

// AddPluginFilters adds filters from a plugin
func AddPluginFilters(filters map[string]interface{}) {
	for name, filter := range filters {
		// Skip filters that are not of type Filter or already in the filter map
		if _, ok := filter.(Filter); !ok {
			logging.Warn("Skipping plugin filter not implementing Filter", "filter", name, "type", fmt.Sprintf("%T", filter))
			continue
		}
		if filterMap[name] != nil {
			logging.Warn("Skipping plugin filter with already registered name", "filter", name)
			continue
		}

		filterMap[name] = func(token string) Filter {
			return cloneFilter(filter.(Filter))
		}
	}
}

// cloneFilter returns a shallow copy of a filter provided by a pointer, so that parameters
// accepted by one usage of the filter do not affect other usages
func cloneFilter(f Filter) Filter {
	v := reflect.ValueOf(f)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		// Filters provided by value are copied when their methods are called anyway
		return f
	}

	clone := reflect.New(v.Elem().Type())
	clone.Elem().Set(v.Elem())
	return clone.Interface().(Filter)
}
