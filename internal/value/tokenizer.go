package value

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bzick/tokenizer"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/denglertai/gonfig/pkg/logging"
)

// Values are processed in two stages:
//  1. findPlaceholders finds the placeholders "${...}" within a value, everything else is kept as is.
//     A placeholder without a closing bracket is an error.
//  2. filterParser splits the content of a placeholder, e.g. "ENV_VAR | filter1 | filter2(param)",
//     into the name of the environment variable, the filter names, their params and the separators between them.
//
// Finding the placeholders does not use a tokenizer, because tokenizing all the text around
// the placeholders is expensive and most values do not contain any placeholders at all.
var filterParser *tokenizer.Tokenizer

const (
	// TokenFilterSeparator is the pipe separating the filters of a placeholder.
	// The tokenizer reserves the key 0 for undefined tokens and ignores definitions using it, so the keys start at 1.
	TokenFilterSeparator = iota + 1
	// TokenParam is a framed string containing the params "(...)" of a filter
	TokenParam
)

func init() {
	filterParser = tokenizer.New()
	filterParser.DefineTokens(TokenFilterSeparator, []string{"|", " |", " | "})
	// A framed string ends at the first unescaped closing bracket, so brackets cannot be nested.
	// Allow escaping the closing brackets, e.g. for regex groups within filter params
	filterParser.DefineStringToken(TokenParam, "(", ")").SetEscapeSymbol('\\')
	// Allow names like MY_VAR_2, base64_decode and template references like @secret to be parsed as a single keyword
	filterParser.AllowKeywordSymbols([]rune{'_', '@'}, tokenizer.Numbers)
}

// ProcessValue replaces all placeholders within the value by the result of their filters.
// If a single placeholder makes up the whole value, the result of its filters is returned as is,
// which may be of another type than string (e.g. to_int). Otherwise, the result is a string
// and results of other types are formatted as text.
func ProcessValue(value string) (any, error) {
	return processValue(value, false)
}

// Mask replaces the results of placeholders in masked values, see ProcessMaskedValue
const Mask = "***"

// ProcessMaskedValue processes the value like ProcessValue, but replaces the result of each placeholder by Mask,
// since it may contain secrets. The filters are still applied, so missing variables and errors are found as well.
// Fragments are returned as is, as their structure is masked by the file they are inserted into.
func ProcessMaskedValue(value string) (any, error) {
	return processValue(value, true)
}

// processValue replaces all placeholders within the value by the result of their filters, or by Mask if masked
func processValue(value string, masked bool) (any, error) {
	// Most values do not contain any placeholders, so return them without any further processing
	if !strings.Contains(value, "${") {
		return value, nil
	}

	placeholders, err := findPlaceholders(value)
	if err != nil {
		return nil, err
	}
	if len(placeholders) == 0 {
		return value, nil
	}

	// Keep the type of the result, so file types supporting it can write e.g. numbers as such
	if len(placeholders) == 1 && placeholders[0].start == 0 && placeholders[0].end == len(value) {
		filtered, err := applyFilters(placeholders[0])
		if _, isFragment := filtered.(filter.Fragment); err == nil && masked && !isFragment {
			return Mask, nil
		}
		return filtered, err
	}

	// Build the result in a single pass, copying the text between the placeholders as is
	var result strings.Builder
	result.Grow(len(value))

	last := 0

	for _, p := range placeholders {
		filtered, err := applyFilters(p)
		if err != nil {
			return nil, err
		}

		// A fragment is a structure, which cannot be embedded into text
		if _, isFragment := filtered.(filter.Fragment); isFragment {
			return nil, fmt.Errorf("the fragment of %s has to make up the whole value", p.token)
		}

		// Results of other types than string are embedded into the surrounding text as text
		s, ok := filtered.(string)
		switch {
		case masked:
			s = Mask
		case !ok:
			s = fmt.Sprint(filtered)
		}

		result.WriteString(value[last:p.start])
		result.WriteString(s)
		last = p.end
	}

	result.WriteString(value[last:])
	return result.String(), nil
}

// applyFilters applies the filters of the placeholder
func applyFilters(p placeholder) (any, error) {
	logging.Debug("Found placeholder", "placeholder", p.token, "start", p.start, "end", p.end)

	// The first filter reads the environment variable and ignores its input, so the token is just a placeholder value.
	// Without any filters, e.g. for "${}", the token is kept as is.
	return filter.ApplyFilters(p.token, parseFilters(p.content()))
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
// cannot be nested. A placeholder without a closing bracket is an error, e.g. a typo like "${VAR | upper",
// since it would otherwise be written to the output as is. The placeholders before it are returned as well.
func findPlaceholders(value string) ([]placeholder, error) {
	var placeholders []placeholder

	for offset := 0; offset < len(value); {
		i := strings.Index(value[offset:], "${")
		if i == -1 {
			break
		}

		start := offset + i
		end := closingBracket(value, start+2)
		if end == -1 {
			return placeholders, fmt.Errorf("unterminated placeholder %q, the closing } is missing", excerpt(value[start:]))
		}

		placeholders = append(placeholders, placeholder{
			token: value[start:end],
			start: start,
			end:   end,
		})
		offset = end
	}

	return placeholders, nil
}

// maxExcerptLength is the number of characters of an unterminated placeholder quoted in errors,
// since the rest of the value may be long, e.g. a whole fragment
const maxExcerptLength = 40

// excerpt returns the beginning of the text, shortened to maxExcerptLength characters followed by "..."
func excerpt(text string) string {
	runes := []rune(text)
	if len(runes) <= maxExcerptLength {
		return text
	}
	return string(runes[:maxExcerptLength]) + "..."
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

// chainStep is a step of the filter chain of a placeholder: the environment variable or placeholder template
// at the start, followed by filters and filter templates
type chainStep struct {
	// name is the name of the environment variable, filter or template. Names of templates start with "@".
	name string
	// params are the params given in brackets, or nil if there are none
	params map[string]string
}

// scanChain splits the content of a placeholder, e.g. "ENV_VAR | filter(key=value)", into its steps.
// If withSource is false, the content is a filter chain without an environment variable, e.g. the filters of a template.
func scanChain(content string, withSource bool) []chainStep {
	steps := make([]chainStep, 0)

	stream := filterParser.ParseString(content)
	defer stream.Close()

	for ; stream.IsValid(); stream.GoNext() {
		token := stream.CurrentToken()

		switch {
		case token.Is(TokenFilterSeparator):
			continue
		case withSource && len(steps) == 0:
			// The first token is always the name of the environment variable or placeholder template
			steps = append(steps, chainStep{name: token.ValueString()})
		case token.Is(tokenizer.TokenKeyword):
			// Other tokens have to be filters or filter templates
			steps = append(steps, chainStep{name: token.ValueString()})
		case token.Is(tokenizer.TokenString) && token.StringSettings().Key == TokenParam && len(steps) > 0:
			// Params always follow the step they belong to
			steps[len(steps)-1].params = parseParams(token.ValueString())
		}
	}

	return steps
}

// parseFilters parses the content of a placeholder, e.g. "ENV_VAR | filter(key=value)", into the filters to apply
func parseFilters(content string) []filter.Filter {
	steps := scanChain(content, true)
	if len(steps) == 0 {
		return []filter.Filter{}
	}

	return append(sourceFilters(steps[0], chainHasDefault(steps[1:])), chainFilters(steps[1:])...)
}

// sourceFilters returns the filters reading the value of a placeholder, from either an environment variable or a placeholder template.
// hasDefault marks chains providing a default, which never fail for a missing variable. Params given for the source are ignored.
func sourceFilters(step chainStep, hasDefault bool) []filter.Filter {
	if name, isTemplate := templateName(step.name); isTemplate {
		return []filter.Filter{placeholderTemplateFilter(name, hasDefault)}
	}

	return []filter.Filter{filter.NewEnvVarFilter(step.name, hasDefault), filter.NewFileInterceptorFilter()}
}

// chainHasDefault returns whether the steps of a filter chain provide a default for a missing variable,
// with the default filter either in the chain itself or within a filter template
func chainHasDefault(steps []chainStep) bool {
	for _, step := range steps {
		if step.name == "default" {
			return true
		}

		// Templates are validated on startup, so references within them cannot form cycles
		if name, isTemplate := templateName(step.name); isTemplate {
			if template, found := filterTemplates[name]; found && chainHasDefault(scanChain(template.Filters, false)) {
				return true
			}
		}
	}
	return false
}

// chainFilters creates the filters of the steps of a filter chain
func chainFilters(steps []chainStep) []filter.Filter {
	filters := make([]filter.Filter, 0, len(steps))

	for _, step := range steps {
		// Filter templates are applied as a single filter, which ignores params
		if name, isTemplate := templateName(step.name); isTemplate {
			filters = append(filters, filterTemplateFilter(name))
			continue
		}

		f := filter.NewFilter(step.name)
		// Params a filter does not declare fail processing, e.g. typos in param names, instead of being ignored
		if err := filter.ValidateParams(step.name, f, step.params); err != nil {
			filters = append(filters, filter.NewErrorFilter(err))
			continue
		}
		// Filters not accepting params ignore them
		if paramsFilter, acceptsParams := f.(filter.FilterParams); acceptsParams && step.params != nil {
			paramsFilter.AcceptParams(step.params)
		}
		filters = append(filters, f)
	}

	return filters
}
