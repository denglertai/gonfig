package value

import (
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

	for stream.IsValid() {
		currentToken := stream.CurrentToken()

		if currentToken.Is(tokenizer.TokenKeyword) && isFirstKeywordToken {
			// Handle key as env var source
			isFirstKeywordToken = false
			token := currentToken.ValueString()
			filters = append(filters, filter.NewEnvVarFilter(token), filter.NewFileInterceptorFilter())
		} else if currentToken.Is(tokenizer.TokenKeyword) && !isFirstKeywordToken {
			// Handle key as filter
			token := currentToken.ValueString()
			filters = append(filters, filter.NewFilter(token))
		} else if currentToken.Is(TokenRoundOpen) {
			// Filter Params Start
		} else if currentToken.Is(TokenRoundClose) {
			// Filter Params End
		} else if currentToken.Is(TokenFilterSeparator) {
			// We have reached a filter separator, we can add the filter to the list
			// filters = append(filters, filter.NewFilter())
		}

		stream.GoNext()
	}

	return filters, nil
}
