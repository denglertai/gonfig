package value

import (
	"fmt"
	"regexp"

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
	parser.DefineStringToken(TokenParam, "${", "}")

	filterParser = tokenizer.New()
	filterParser.DefineTokens(TokenFilterSeparator, []string{"|", " |", " | "})
	filterParser.DefineStringToken(TokenParam, "(", ")")
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

	asRunes := []rune(input)
	before := string(asRunes[0 : t.start+offset])
	after := string(asRunes[t.end+offset:])

	switch result.(type) {
	case string:
		result := fmt.Sprintf("%s%s%s", before, result, after)

		return result, len(result) - lenBefore, nil
	default:
		return result, 0, nil
	}
}

var kvPairRe = regexp.MustCompile(`(.*?)=([^=]*)(?:,|$)`)

// parseKV parses a key value string into a map
func parseKV(kvStr string) map[string]string {
	// remove leading and trailing brackets
	kvStr = kvStr[1 : len(kvStr)-1]

	res := map[string]string{}
	for _, kv := range kvPairRe.FindAllStringSubmatch(kvStr, -1) {
		res[kv[1]] = kv[2]
	}
	return res
}

func processStream(stream *tokenizer.Stream) ([]ApplyableTokenParam, error) {
	logging.Trace("Processing stream", "stream", stream)

	result := make([]ApplyableTokenParam, 0)

	for stream.IsValid() {
		currentToken := stream.CurrentToken()

		if currentToken.Is(tokenizer.TokenString) {
			if currentToken.StringSettings().Key == TokenParam {
				param := TokenFilterParam{
					token:   currentToken.ValueString(),
					start:   currentToken.Offset(),
					end:     currentToken.Offset() + len(currentToken.ValueString()),
					filters: make([]filter.Filter, 0),
				}

				filterStream := filterParser.ParseString(param.token[2 : len(param.token)-1])

				defer filterStream.Close()
				for filterStream.IsValid() {
					filterToken := filterStream.CurrentToken()

					// Skip filter separator
					if filterToken.Is(TokenFilterSeparator) {
						filterStream.GoNext()
						continue
					}

					// The first token is always the name of the env var
					// Thus append the env var filter and the file interceptor filter
					if len(param.filters) == 0 {
						param.filters = append(param.filters, filter.NewEnvVarFilter(filterToken.ValueString()), filter.NewFileInterceptorFilter())
					} else if filterToken.Is(tokenizer.TokenKeyword) {
						// Other tokens have to be filters
						param.filters = append(param.filters, filter.NewFilter(filterToken.ValueString()))
					} else if filterToken.Is(tokenizer.TokenString) && filterToken.StringSettings().Key == TokenParam {
						// Parse the Params for the filter and apply it to the last generated filter
						lastFilter, acceptsParams := param.filters[len(param.filters)-1].(filter.FilterParams)
						if acceptsParams {
							params := parseKV(filterToken.ValueString())
							lastFilter.AcceptParams(params)
						}
					}

					filterStream.GoNext()
				}

				logging.Debug("Found param", "param", param.token, "start", param.start, "end", param.end)

				result = append(result, param)
			}
		}

		stream.GoNext()
	}

	return result, nil
}
