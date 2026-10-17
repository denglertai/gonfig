package value

import (
	"strconv"
	"strings"

	"github.com/bzick/tokenizer"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
)

// Values are processed in two stages:
//  1. findPlaceholders finds the placeholders "${...}" within a value, everything else is kept as is.
//  2. filterParser splits the content of a placeholder, e.g. "ENV_VAR | filter1 | filter2(param)",
//     into the name of the environment variable, the filter names, their params and the separators between them.
//
// Finding the placeholders does not use a tokenizer, because tokenizing all the text around
// the placeholders is expensive and most values do not contain any placeholders at all.
var filterParser *tokenizer.Tokenizer

const (
	// TokenFilterSeparator is the pipe separating the filters of a placeholder
	TokenFilterSeparator = iota
	// TokenParam is a framed string containing the params "(...)" of a filter
	TokenParam
)

func init() {
	filterParser = tokenizer.New()
	filterParser.DefineTokens(TokenFilterSeparator, []string{"|", " |", " | "})
	// A framed string ends at the first unescaped closing bracket, so brackets cannot be nested.
	// Allow escaping the closing brackets, e.g. for regex groups within filter params
	filterParser.DefineStringToken(TokenParam, "(", ")").SetEscapeSymbol('\\')
	// Allow names like MY_VAR_2 and base64_decode to be parsed as a single keyword
	filterParser.AllowKeywordSymbols(tokenizer.Underscore, tokenizer.Numbers)
}

// ProcessValue replaces all placeholders within the value by the result of their filters.
// The result is a string, unless a placeholder's filters return another type (e.g. to_int),
// in which case that result replaces the whole value.
func ProcessValue(value string) (any, error) {
	// Most values do not contain any placeholders, so return them without any further processing
	if !strings.Contains(value, "${") {
		return value, nil
	}

	placeholders := findPlaceholders(value)
	if len(placeholders) == 0 {
		return value, nil
	}

	// Build the result in a single pass, copying the text between the placeholders as is
	var result strings.Builder
	result.Grow(len(value))

	var nonStringResult any
	lastIsNonString := false
	last := 0

	for _, p := range placeholders {
		logging.Debug("Found placeholder", "placeholder", p.token, "start", p.start, "end", p.end)

		// The first filter reads the environment variable and ignores its input, so the token is just a placeholder value.
		// Without any filters, e.g. for "${}", the token is kept as is.
		filtered, err := filter.ApplyFilters(p.token, parseFilters(p.content()))
		if err != nil {
			return nil, err
		}

		result.WriteString(value[last:p.start])
		if s, ok := filtered.(string); ok {
			result.WriteString(s)
			lastIsNonString = false
		} else {
			// Only strings can be embedded into the surrounding text. Other types replace the whole value
			// if they are the result of the last placeholder, otherwise the placeholder is kept as is.
			result.WriteString(p.token)
			nonStringResult = filtered
			lastIsNonString = true
		}
		last = p.end
	}

	if lastIsNonString {
		return nonStringResult, nil
	}

	result.WriteString(value[last:])
	return result.String(), nil
}

// placeholder is a placeholder "${...}" found within a value
type placeholder struct {
	// token is the placeholder including "${" and "}"
	token string
	// start and end are the byte offsets of the placeholder within the value
	start int
	end   int
}

// content returns the content of the placeholder without "${" and "}"
func (p placeholder) content() string {
	return p.token[2 : len(p.token)-1]
}

// findPlaceholders returns the placeholders within the value in the order they appear.
// A placeholder ends at the first closing bracket that is not escaped by a backslash, so placeholders
// cannot be nested. A placeholder without a closing bracket is not a placeholder and kept as is.
func findPlaceholders(value string) []placeholder {
	var placeholders []placeholder

	for offset := 0; offset < len(value); {
		i := strings.Index(value[offset:], "${")
		if i == -1 {
			break
		}

		start := offset + i
		end := closingBracket(value, start+2)
		if end == -1 {
			break
		}

		placeholders = append(placeholders, placeholder{
			token: value[start:end],
			start: start,
			end:   end,
		})
		offset = end
	}

	return placeholders
}

// closingBracket returns the offset after the first unescaped "}" at or after from, or -1 if there is none
func closingBracket(value string, from int) int {
	for i := from; i < len(value); i++ {
		switch value[i] {
		case '\\':
			// Skip the escaped character
			i++
		case '}':
			return i + 1
		}
	}
	return -1
}

// paramEscapes are the characters which may be escaped by a backslash within filter params
const paramEscapes = `,=(){}\`

// parseParams parses filter params, e.g. "(key=value,positional)", into a map.
// Named params are stored by their key, positional params by their position ("0", "1", ...).
// A backslash escapes the following character if it is one of paramEscapes, otherwise it is kept.
func parseParams(paramStr string) map[string]string {
	// Remove the leading and trailing brackets. The trailing one is missing if the params are not closed, e.g. "${VAL | replace(}"
	paramStr = strings.TrimPrefix(paramStr, "(")
	paramStr = strings.TrimSuffix(paramStr, ")")

	res := map[string]string{}
	if paramStr == "" {
		return res
	}

	var current strings.Builder
	key := ""
	hasKey := false
	position := 0

	flush := func() {
		if !hasKey {
			key = strconv.Itoa(position)
		}
		res[key] = current.String()
		current.Reset()
		hasKey = false
		position++
	}

	for i := 0; i < len(paramStr); i++ {
		c := paramStr[i]
		switch {
		case c == '\\' && i+1 < len(paramStr):
			i++
			if !strings.ContainsRune(paramEscapes, rune(paramStr[i])) {
				current.WriteByte(c)
			}
			current.WriteByte(paramStr[i])
		case c == ',':
			flush()
		case c == '=' && !hasKey:
			key = current.String()
			hasKey = true
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	flush()

	return res
}

// parseFilters parses the content of a placeholder, e.g. "ENV_VAR | filter(key=value)", into the filters to apply
func parseFilters(content string) []filter.Filter {
	filters := make([]filter.Filter, 0)

	filterStream := filterParser.ParseString(content)
	defer filterStream.Close()

	for ; filterStream.IsValid(); filterStream.GoNext() {
		filterToken := filterStream.CurrentToken()

		// Skip filter separator
		if filterToken.Is(TokenFilterSeparator) {
			continue
		}

		// The first token is always the name of the env var
		// Thus append the env var filter and the file interceptor filter
		if len(filters) == 0 {
			filters = append(filters, filter.NewEnvVarFilter(filterToken.ValueString()), filter.NewFileInterceptorFilter())
		} else if filterToken.Is(tokenizer.TokenKeyword) {
			// Other tokens have to be filters
			filters = append(filters, filter.NewFilter(filterToken.ValueString()))
		} else if filterToken.Is(tokenizer.TokenString) && filterToken.StringSettings().Key == TokenParam {
			// Params always follow the filter they belong to, so apply them to the last generated filter.
			// Filters not accepting params, including the environment variable filters, ignore them.
			lastFilter, acceptsParams := filters[len(filters)-1].(filter.FilterParams)
			if acceptsParams {
				params := parseParams(filterToken.ValueString())
				lastFilter.AcceptParams(params)
			}
		}
	}

	return filters
}
