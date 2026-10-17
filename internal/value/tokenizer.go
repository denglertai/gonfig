package value

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bzick/tokenizer"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
)

var parser *tokenizer.Tokenizer
var filterParser *tokenizer.Tokenizer

const (
	TokenFilterSeparator = iota
	TokenParam
)

func init() {
	parser = tokenizer.New()
	// Allow escaping the closing brackets, e.g. for regex quantifiers within filter params
	parser.DefineStringToken(TokenParam, "${", "}").SetEscapeSymbol('\\')

	filterParser = tokenizer.New()
	filterParser.DefineTokens(TokenFilterSeparator, []string{"|", " |", " | "})
	filterParser.DefineStringToken(TokenParam, "(", ")").SetEscapeSymbol('\\')
	filterParser.AllowKeywordSymbols(tokenizer.Underscore, tokenizer.Numbers)
}

// Tokenize returns a stream of tokens
func tokenize(value string) (*tokenizer.Stream, error) {
	return parser.ParseString(value), nil
}

// ProcessValue takes the input value and processes it as needed
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

type ApplyableTokenParam interface {
	// Apply applies the token to the input string
	Apply(input string, offset int) (any, int, error)
}

type TokenFilterParam struct {
	token   string
	filters []filter.Filter
	start   int
	end     int
}

// Apply applies the token to the input string and returns the result, the length difference and an error if any
func (t TokenFilterParam) Apply(input string, offset int) (any, int, error) {
	result, err := filter.ApplyFilters(t.token, t.filters)

	if err != nil {
		return "", 0, err
	}

	lenBefore := len(input)

	// start and end are byte offsets, so slice the bytes rather than the runes
	before := input[:t.start+offset]
	after := input[t.end+offset:]

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

func processStream(stream *tokenizer.Stream) ([]ApplyableTokenParam, error) {
	logging.Trace("Processing stream", "stream", stream)

	result := make([]ApplyableTokenParam, 0)

	for stream.IsValid() {
		currentToken := stream.CurrentToken()

		if currentToken.Is(tokenizer.TokenString) {
			if currentToken.StringSettings().Key == TokenParam {
				token := currentToken.ValueString()
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
			// Parse the Params for the filter and apply it to the last generated filter
			lastFilter, acceptsParams := filters[len(filters)-1].(filter.FilterParams)
			if acceptsParams {
				params := parseParams(filterToken.ValueString())
				lastFilter.AcceptParams(params)
			}
		}
	}

	return filters
}
