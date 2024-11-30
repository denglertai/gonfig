package value

import (
	"fmt"

	"github.com/bzick/tokenizer"
	"github.com/denglertai/gonfig/internal/filter"
)

var parser *tokenizer.Tokenizer

const (
	TokenDollar = iota + 1
	TokenCurlyOpen
	TokenCurlyClose
	TokenRoundOpen
	TokenRoundClose
	TokenFilterSeparator
	TokenEqual
)

func init() {
	parser = tokenizer.New()
	parser.DefineTokens(TokenDollar, []string{"$"})
	parser.DefineTokens(TokenCurlyOpen, []string{"{"})
	parser.DefineTokens(TokenCurlyClose, []string{"}"})
	parser.DefineTokens(TokenRoundOpen, []string{"("})
	parser.DefineTokens(TokenRoundClose, []string{")"})
	parser.DefineTokens(TokenEqual, []string{"="})
	parser.DefineTokens(TokenFilterSeparator, []string{"|", " |", " | "})

	parser.AllowKeywordUnderscore()
}

// Tokenize returns a stream of tokens
func tokenize(value string) (*tokenizer.Stream, error) {
	return parser.ParseString(value), nil
}

func ProcessValue(value string) (interface{}, error) {
	stream, err := tokenize(value)
	if err != nil {
		return nil, err
	}

	defer stream.Close()

	filters, err := processStream(stream)

	if err != nil {
		return value, err
	}

	return filter.ApplyFilters(value, filters)
}

func processStream(stream *tokenizer.Stream) ([]filter.Filter, error) {
	var filters []filter.Filter

	isFirstKeywordToken := true

	var currentFilter filter.Filter
	var currentParams map[string]string = make(map[string]string)
	var currentParamKey string
	var currentParamValue string
	var equalHit bool

	for stream.IsValid() {
		currentToken := stream.CurrentToken()

		if currentToken.Is(tokenizer.TokenKeyword) && isFirstKeywordToken {
			// Handle key as env var source
			isFirstKeywordToken = false
			token := currentToken.ValueString()
			filters = append(filters, filter.NewEnvVarFilter(token), filter.NewFileInterceptorFilter())
		} else if currentToken.Is(tokenizer.TokenKeyword) && !isFirstKeywordToken {
			token := currentToken.ValueString()
			if currentFilter == nil {
				// Handle key as filter
				currentFilter = filter.NewFilter(token)
			} else if currentParamKey == "" {
				// Handle key as filter param
				currentParamKey = token
			} else {
				currentParamValue = token
			}
		} else if currentToken.Is(TokenRoundOpen) {
			// Filter Params Start
			currentParamKey = ""
		} else if currentToken.Is(TokenEqual) {
			equalHit = true
		} else if currentToken.Is(tokenizer.TokenString) && equalHit {
			// Filter Param Value
			currentParamValue = currentToken.ValueString()
			equalHit = false
		} else if currentToken.Is(TokenRoundClose) {
			// Filter Params End
			if currentParamKey != "" {
				currentParams[currentParamKey] = currentParamValue
			}
		} else if currentFilter != nil && (currentToken.Is(TokenFilterSeparator) || currentToken.Is(TokenCurlyClose)) {
			// We have reached a filter separator or the end, we can add the filter to the list
			if p, ok := currentFilter.(filter.FilterParams); ok && currentParams != nil {
				p.AcceptParams(currentParams)
				currentParams = make(map[string]string)
			}

			filters = append(filters, currentFilter)
			currentFilter = nil
		}

		fmt.Println(currentToken)

		stream.GoNext()
	}

	return filters, nil
}
