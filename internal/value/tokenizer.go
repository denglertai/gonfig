package value

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bzick/tokenizer"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
)

// Values are tokenized in two stages:
//  1. parser finds the placeholders "${...}" within a value, everything else is kept as is.
//  2. filterParser splits the content of a placeholder, e.g. "ENV_VAR | filter1 | filter2(param)",
//     into the name of the environment variable, the filter names, their params and the separators between them.
var parser *tokenizer.Tokenizer
var filterParser *tokenizer.Tokenizer

const (
	// TokenFilterSeparator is the pipe separating the filters of a placeholder
	TokenFilterSeparator = iota
	// TokenParam is a framed string: a placeholder "${...}" for parser, the params "(...)" of a filter for filterParser
	TokenParam
)

func init() {
	parser = tokenizer.New()
	// A framed string ends at the first unescaped closing bracket, so brackets cannot be nested.
	// Allow escaping the closing brackets, e.g. for regex quantifiers within filter params
	parser.DefineStringToken(TokenParam, "${", "}").SetEscapeSymbol('\\')

	filterParser = tokenizer.New()
	filterParser.DefineTokens(TokenFilterSeparator, []string{"|", " |", " | "})
	filterParser.DefineStringToken(TokenParam, "(", ")").SetEscapeSymbol('\\')
	// Allow names like MY_VAR_2 and base64_decode to be parsed as a single keyword
	filterParser.AllowKeywordSymbols(tokenizer.Underscore, tokenizer.Numbers)
}

// Tokenize returns a stream of tokens
func tokenize(value string) (*tokenizer.Stream, error) {
	return parser.ParseString(value), nil
}

// ProcessValue replaces all placeholders within the value by the result of their filters.
// The result is a string, unless a placeholder's filters return another type (e.g. to_int),
// in which case that result replaces the whole value.
func ProcessValue(value string) (any, error) {
	stream, err := tokenize(value)
	if err != nil {
		return nil, err
	}

	defer stream.Close()

	params, err := processStream(stream)

	if err != nil {
		return value, err
	}

	// The start and end offsets of the placeholders refer to the original value.
	// Every replacement changes the length of the value, so sumDiff tracks the total change
	// to shift the offsets of the following placeholders accordingly.
	sumDiff := 0
	result := value
	var finalResult any

	// If there are no params return the value as is without any processing
	if len(params) == 0 {
		return value, nil
	}

	for _, param := range params {
		paramResult, lenDiff, err := param.Apply(result, sumDiff)
		if err != nil {
			return nil, err
		}

		sumDiff += lenDiff
		if stringResult, ok := paramResult.(string); ok {
			logging.Debug("Result", "result", result)
			result = stringResult
			finalResult = stringResult
		} else {
			finalResult = paramResult
		}
	}

	return finalResult, nil
}

// ApplyableTokenParam is a placeholder found within a value
type ApplyableTokenParam interface {
	// Apply applies the token to the input string
	Apply(input string, offset int) (any, int, error)
}

// TokenFilterParam is a placeholder "${...}" and the filters parsed from its content
type TokenFilterParam struct {
	// token is the placeholder including "${" and "}"
	token string
	// filters starts with the filters reading the environment variable, followed by the filters of the placeholder
	filters []filter.Filter
	// start and end are the byte offsets of the placeholder within the original value
	start int
	end   int
}

// Apply applies the token to the input string and returns the result, the length difference and an error if any
func (t TokenFilterParam) Apply(input string, offset int) (any, int, error) {
	// The first filter reads the environment variable and ignores its input, so the token is just a placeholder value
	result, err := filter.ApplyFilters(t.token, t.filters)

	if err != nil {
		return "", 0, err
	}

	lenBefore := len(input)

	// start and end are byte offsets, so slice the bytes rather than the runes
	before := input[:t.start+offset]
	after := input[t.end+offset:]

	// Only strings can be embedded into the surrounding text, other types replace the whole value
	switch result.(type) {
	case string:
		result := fmt.Sprintf("%s%s%s", before, result, after)

		return result, len(result) - lenBefore, nil
	default:
		return result, 0, nil
	}
}

// paramEscapes are the characters which may be escaped by a backslash within filter params
const paramEscapes = `,=(){}\`

// parseParams parses filter params, e.g. "(key=value,positional)", into a map.
// Named params are stored by their key, positional params by their position ("0", "1", ...).
// A backslash escapes the following character if it is one of paramEscapes, otherwise it is kept.
func parseParams(paramStr string) map[string]string {
	// remove leading and trailing brackets
	paramStr = paramStr[1 : len(paramStr)-1]

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

// processStream collects the placeholders of the token stream created by parser and parses their filters
func processStream(stream *tokenizer.Stream) ([]ApplyableTokenParam, error) {
	logging.Trace("Processing stream", "stream", stream)

	result := make([]ApplyableTokenParam, 0)

	for stream.IsValid() {
		currentToken := stream.CurrentToken()

		// Only placeholders are of interest, the text around them is kept as is
		if currentToken.Is(tokenizer.TokenString) {
			if currentToken.StringSettings().Key == TokenParam {
				token := currentToken.ValueString()
				// Strip "${" and "}" to get the content of the placeholder for parsing its filters
				param := TokenFilterParam{
					token:   token,
					start:   currentToken.Offset(),
					end:     currentToken.Offset() + len(token),
					filters: parseFilters(token[2 : len(token)-1]),
				}

				logging.Debug("Found param", "param", param.token, "start", param.start, "end", param.end)

				result = append(result, param)
			}
		}

		stream.GoNext()
	}

	return result, nil
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
