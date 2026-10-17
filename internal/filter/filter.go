package filter

import (
	"cmp"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
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

// failOnMissing makes environment variables that are not set fail processing, see SetFailOnMissing
var failOnMissing bool

// SetFailOnMissing sets whether environment variables that are not set fail processing instead of becoming empty strings.
// Placeholders providing a default, e.g. with the default filter, never fail for missing variables.
func SetFailOnMissing(enabled bool) {
	failOnMissing = enabled
}

// FailOnMissing returns whether environment variables that are not set fail processing
func FailOnMissing() bool {
	return failOnMissing
}

// EnvVarFilter is a filter that replaces the value with the value of an environment variable
type EnvVarFilter struct {
	envVar string
	// hasDefault marks placeholders providing a default, which never fail for a missing variable
	hasDefault bool
}

// Process replaces the value with the value of an environment variable.
// A variable that is not set becomes an empty string, or fails if enabled by SetFailOnMissing and there is no default.
func (f *EnvVarFilter) Process(_ any) (any, error) {
	logging.Debug("Processing EnvVarFilter", "envVar", f.envVar)

	value, found := os.LookupEnv(f.envVar)
	if !found {
		if failOnMissing && !f.hasDefault {
			return "", fmt.Errorf("environment variable %q is not set", f.envVar)
		}
		return "", nil
	}

	return value, nil
}

// NewEnvVarFilter creates a new EnvVarFilter. hasDefault marks placeholders providing a default for a missing variable.
func NewEnvVarFilter(envVar string, hasDefault bool) *EnvVarFilter {
	return &EnvVarFilter{
		envVar:     envVar,
		hasDefault: hasDefault,
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

// Fragment is a structure to be inserted into a config file, e.g. a YAML mapping or XML elements.
// The text is parsed by the file type the fragment is inserted into.
type Fragment struct {
	Text string
	// IsList marks fragments created from lists, e.g. of split, whose text is a JSON array.
	// They can only be inserted into file types supporting lists, like YAML and JSON.
	IsList bool
}

// String returns the text of the fragment, e.g. for filters following the fragment filter
func (f Fragment) String() string {
	return f.Text
}

// DefaultValueFilter replaces empty values, e.g. of environment variables that are not set, by a default value
type DefaultValueFilter struct {
	value string
}

// Process returns the default value if the value is an empty string, otherwise the value unchanged
func (f *DefaultValueFilter) Process(value any) (any, error) {
	if s, ok := value.(string); ok && s == "" {
		return f.value, nil
	}
	return value, nil
}

// NewDefaultValueFilter creates a new DefaultValueFilter
func NewDefaultValueFilter(value string) *DefaultValueFilter {
	return &DefaultValueFilter{
		value: value,
	}
}

// ChainFilter applies a chain of filters as a single filter, e.g. for templates
type ChainFilter struct {
	filters []Filter
}

// Process applies the filters of the chain
func (f *ChainFilter) Process(value any) (any, error) {
	return ApplyFilters(value, f.filters)
}

// NewChainFilter creates a new ChainFilter
func NewChainFilter(filters []Filter) *ChainFilter {
	return &ChainFilter{
		filters: filters,
	}
}

// ErrorFilter fails with an error, e.g. for references to templates that do not exist
type ErrorFilter struct {
	err error
}

// Process returns the error of the filter
func (f *ErrorFilter) Process(_ any) (any, error) {
	return "", f.err
}

// NewErrorFilter creates a new ErrorFilter
func NewErrorFilter(err error) *ErrorFilter {
	return &ErrorFilter{
		err: err,
	}
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

// toFloat converts a filter input to a finite float64, parsing string values
func toFloat(value any) (float64, error) {
	var f float64
	switch v := value.(type) {
	case float64:
		f = v
	case int:
		f = float64(v)
	case string:
		var err error
		if f, err = strconv.ParseFloat(v, 64); err != nil {
			return 0, err
		}
	default:
		return 0, fmt.Errorf("cannot convert %v (%T) to float", value, value)
	}

	// Config files cannot represent infinite numbers and NaN consistently
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, fmt.Errorf("cannot convert %v to a finite float", value)
	}
	return f, nil
}

// toBool converts a filter input to a bool, parsing string values like "true", "false", "1" and "0"
func toBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(v)
	default:
		return false, fmt.Errorf("cannot convert %v (%T) to bool", value, value)
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

// UnknownFiltersAllowed returns whether unknown filters pass values on unchanged instead of failing
func UnknownFiltersAllowed() bool {
	return allowUnknownFilters
}

// IsKnown returns whether a filter with the name is registered, either built-in or by a plugin
func IsKnown(name string) bool {
	_, found := filterMap[name]
	return found
}

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
	return f.fn(value, f.params)
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

	filterMap["default"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				// Like the default of placeholder templates, it replaces empty values, e.g. of variables that are not set
				if s, ok := value.(string); ok && s == "" {
					fallback, _ := param(params, "value", 0)
					return fallback, nil
				}
				return value, nil
			},
		}
	}

	filterMap["required"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				if s, ok := value.(string); ok && s == "" {
					return "", errors.New("filter required: the value is empty or the environment variable is not set")
				}
				return value, nil
			},
		}
	}

	filterMap["to_int"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return toInt(value)
			},
		}
	}

	filterMap["to_float"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return toFloat(value)
			},
		}
	}

	filterMap["to_bool"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				return toBool(value)
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

	filterMap["fragment"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, _ map[string]string) (any, error) {
				// Lists, e.g. of split, become JSON arrays, which are valid YAML as well
				if list, ok := value.([]string); ok {
					// Encoding strings cannot fail, invalid UTF-8 is replaced
					data, _ := json.Marshal(list)
					return Fragment{Text: string(data), IsList: true}, nil
				}

				return Fragment{Text: toString(value)}, nil
			},
		}
	}

	filterMap["url_encode"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, params map[string]string) (string, error) {
				mode, _ := param(params, "mode", 0)
				switch mode {
				case "", "query":
					return url.QueryEscape(s), nil
				case "path":
					return url.PathEscape(s), nil
				default:
					return "", fmt.Errorf("filter url_encode: unsupported mode %q, use query or path", mode)
				}
			}),
		}
	}

	filterMap["url_decode"] = func(token string) Filter {
		return &FuncFilter{
			fn: stringFunc(func(s string, params map[string]string) (string, error) {
				mode, _ := param(params, "mode", 0)
				var decoded string
				var err error
				switch mode {
				case "", "query":
					decoded, err = url.QueryUnescape(s)
				case "path":
					decoded, err = url.PathUnescape(s)
				default:
					return "", fmt.Errorf("filter url_decode: unsupported mode %q, use query or path", mode)
				}
				if err != nil {
					return "", fmt.Errorf("filter url_decode: %w", err)
				}
				return decoded, nil
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
				// An empty delimiter, e.g. of split(,) whose comma separates two empty params, uses the default as well
				delimiter, _ := param(params, "delimiter", 0)
				delimiter = cmp.Or(delimiter, defaultListDelimiter)

				// An empty value is an empty list, e.g. for fragments
				s := toString(value)
				if s == "" {
					return []string{}, nil
				}
				return strings.Split(s, delimiter), nil
			},
		}
	}

	filterMap["join"] = func(token string) Filter {
		return &FuncFilter{
			fn: func(value any, params map[string]string) (any, error) {
				// An empty delimiter, e.g. of join(,) whose comma separates two empty params, uses the default as well
				delimiter, _ := param(params, "delimiter", 0)
				delimiter = cmp.Or(delimiter, defaultListDelimiter)

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
