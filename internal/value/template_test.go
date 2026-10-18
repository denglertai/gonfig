package value

import (
	"testing"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/filter"
	"github.com/stretchr/testify/assert"
)

// setTemplates sets the templates for a test and removes them afterwards
func setTemplates(t *testing.T, templates config.Templates) {
	t.Helper()
	assert.NoError(t, SetTemplates(templates))
	t.Cleanup(func() { assert.NoError(t, SetTemplates(config.Templates{})) })
}

// jdbcTemplates are the templates of the example in the documentation
var jdbcTemplates = config.Templates{
	Filters: map[string]config.FilterTemplate{
		"secret":      {Description: "Trims a secret and encodes it to base64", Filters: "trim | base64"},
		"secret_hash": {Filters: "@secret | md5"},
	},
	Placeholders: map[string]config.PlaceholderTemplate{
		"db_host": {Env: "TPL_DB_HOST", Default: "localhost"},
		"db_port": {Env: "TPL_DB_PORT", Default: "5432"},
		"db_name": {Env: "TPL_DB_NAME", Default: "app", Filters: "trim | lower"},
		"db_url":  {Value: "jdbc:postgresql://${@db_host}:${@db_port}/${@db_name}"},
		"db_user_url": {
			Value:   "${@db_url}?user=${TPL_DB_USER | url_encode}",
			Filters: "trim",
		},
		"db_password": {Env: "TPL_DB_PASSWORD", Filters: "@secret"},
	},
}

func TestProcessValueWithTemplates(t *testing.T) {
	setTemplates(t, jdbcTemplates)

	testCases := []struct {
		desc  string
		env   map[string]string
		value string
		want  any
	}{
		{desc: "filter template", env: map[string]string{"TPL_SECRET": " s3cret "}, value: "${TPL_SECRET | @secret}", want: "czNjcmV0"},
		{desc: "filter template referencing a filter template", env: map[string]string{"TPL_SECRET": "s3cret"}, value: "${TPL_SECRET | @secret_hash}", want: "c8cd34f24d61ba778fe2f942b735a222"},
		{desc: "filter template followed by a filter", env: map[string]string{"TPL_SECRET": "a"}, value: "${TPL_SECRET | @secret | lower}", want: "yq=="},
		{desc: "filter template ignores params", env: map[string]string{"TPL_SECRET": "a"}, value: "${TPL_SECRET | @secret(x)}", want: "YQ=="},
		{desc: "default if not set", value: "${@db_host}", want: "localhost"},
		{desc: "default if empty", env: map[string]string{"TPL_DB_HOST": ""}, value: "${@db_host}", want: "localhost"},
		{desc: "environment variable if set", env: map[string]string{"TPL_DB_HOST": "db"}, value: "${@db_host}", want: "db"},
		{desc: "filters of the template", env: map[string]string{"TPL_DB_NAME": " Orders "}, value: "${@db_name}", want: "orders"},
		{desc: "filters of the template apply to the default", value: "${@db_name | upper}", want: "APP"},
		{desc: "value template", value: "${@db_url}", want: "jdbc:postgresql://localhost:5432/app"},
		{desc: "value template with environment variables", env: map[string]string{"TPL_DB_HOST": "db", "TPL_DB_PORT": "6543"}, value: "${@db_url}", want: "jdbc:postgresql://db:6543/app"},
		{desc: "nested value template", env: map[string]string{"TPL_DB_USER": "a b"}, value: "${@db_user_url}", want: "jdbc:postgresql://localhost:5432/app?user=a+b"},
		{desc: "embedded template", value: "url: ${@db_url}?ssl=true", want: "url: jdbc:postgresql://localhost:5432/app?ssl=true"},
		{desc: "typed result", value: "${@db_port | to_int}", want: 5432},
		{desc: "placeholder template using a filter template", env: map[string]string{"TPL_DB_PASSWORD": " pw "}, value: "${@db_password}", want: "cHc="},
		{desc: "placeholder template ignores params", value: "${@db_host(x)}", want: "localhost"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			for _, key := range []string{"TPL_SECRET", "TPL_DB_HOST", "TPL_DB_PORT", "TPL_DB_NAME", "TPL_DB_USER", "TPL_DB_PASSWORD"} {
				t.Setenv(key, "")
			}
			for key, value := range tC.env {
				t.Setenv(key, value)
			}

			result, err := ProcessValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

func TestProcessValueWithUnknownTemplates(t *testing.T) {
	setTemplates(t, jdbcTemplates)

	_, err := ProcessValue("${@not_defined}")
	assert.EqualError(t, err, `unknown placeholder template "not_defined"`)

	_, err = ProcessValue("${VALUE | @not_defined}")
	assert.EqualError(t, err, `unknown filter template "not_defined"`)

	// Placeholder templates cannot be used as filters
	_, err = ProcessValue("${VALUE | @db_host}")
	assert.EqualError(t, err, `unknown filter template "db_host"`)
}

func TestProcessValueWithoutTemplates(t *testing.T) {
	_, err := ProcessValue("${@db_url}")
	assert.EqualError(t, err, `unknown placeholder template "db_url"`)
}

// TestSetTemplatesValidation checks the errors of invalid filter and placeholder templates
func TestSetTemplatesValidation(t *testing.T) {
	testCases := []struct {
		desc      string
		templates config.Templates
		wantErr   string
	}{
		{
			desc:      "invalid filter template name",
			templates: config.Templates{Filters: map[string]config.FilterTemplate{"db-host": {Filters: "trim"}}},
			wantErr:   `filter template "db-host": invalid name, use lower case letters, digits and underscores`,
		},
		{
			desc:      "invalid placeholder template name",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"Host": {Env: "X"}}},
			wantErr:   `placeholder template "Host": invalid name, use lower case letters, digits and underscores`,
		},
		{
			desc: "name used by both kinds",
			templates: config.Templates{
				Filters:      map[string]config.FilterTemplate{"dup": {Filters: "trim"}},
				Placeholders: map[string]config.PlaceholderTemplate{"dup": {Env: "X"}},
			},
			wantErr: `filter template "dup": the name is also used by a placeholder template`,
		},
		{
			desc:      "empty filters",
			templates: config.Templates{Filters: map[string]config.FilterTemplate{"empty": {Filters: " "}}},
			wantErr:   `filter template "empty": filters must not be empty`,
		},
		{
			desc:      "unknown filter",
			templates: config.Templates{Filters: map[string]config.FilterTemplate{"secret": {Filters: "trim | bas64"}}},
			wantErr:   `filter template "secret": unknown filter "bas64"`,
		},
		{
			desc:      "unknown filter template",
			templates: config.Templates{Filters: map[string]config.FilterTemplate{"secret": {Filters: "@missing"}}},
			wantErr:   `filter template "secret": unknown filter template "missing"`,
		},
		{
			desc: "placeholder template used as filter",
			templates: config.Templates{
				Filters:      map[string]config.FilterTemplate{"secret": {Filters: "@host"}},
				Placeholders: map[string]config.PlaceholderTemplate{"host": {Env: "X"}},
			},
			wantErr: `filter template "secret": "host" is a placeholder template, which can only be used as ${@host}`,
		},
		{
			desc: "cycle of filter templates",
			templates: config.Templates{Filters: map[string]config.FilterTemplate{
				"a": {Filters: "@b"},
				"b": {Filters: "upper | @a"},
			}},
			wantErr: `filter templates refer to each other in a cycle: a -> b -> a`,
		},
		{
			desc:      "env and value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"host": {Env: "X", Value: "y"}}},
			wantErr:   `placeholder template "host": define either env or value, not both`,
		},
		{
			desc:      "neither env nor value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"host": {Default: "x"}}},
			wantErr:   `placeholder template "host": define either env or value`,
		},
		{
			desc:      "default with value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"host": {Value: "x", Default: "y"}}},
			wantErr:   `placeholder template "host": default can only be used with env`,
		},
		{
			desc:      "unknown filter of placeholder template",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"host": {Env: "X", Filters: "uper"}}},
			wantErr:   `placeholder template "host": unknown filter "uper"`,
		},
		{
			desc:      "unknown placeholder template in value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"url": {Value: "${@missing}"}}},
			wantErr:   `placeholder template "url": unknown placeholder template "missing" in value`,
		},
		{
			desc:      "unterminated placeholder in value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"url": {Value: "jdbc:${X | upper"}}},
			wantErr:   `placeholder template "url": unterminated placeholder "${X | upper", the closing } is missing in value`,
		},
		{
			desc:      "unknown param in filter template",
			templates: config.Templates{Filters: map[string]config.FilterTemplate{"secret": {Filters: "trim(both) | base64"}}},
			wantErr:   `filter template "secret": filter trim accepts no params, but got 1`,
		},
		{
			desc:      "unknown param in value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"url": {Value: "${X | default(vaule=x)}"}}},
			wantErr:   `placeholder template "url": filter default has no param "vaule", it accepts value in value`,
		},
		{
			desc:      "unknown filter in value",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"url": {Value: "${X | uper}"}}},
			wantErr:   `placeholder template "url": unknown filter "uper" in value`,
		},
		{
			desc:      "placeholder template referring to itself",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"url": {Value: "${@url}"}}},
			wantErr:   `placeholder templates refer to each other in a cycle: url -> url`,
		},
		{
			desc: "cycle of placeholder templates",
			templates: config.Templates{Placeholders: map[string]config.PlaceholderTemplate{
				"a": {Value: "${@b}"},
				"b": {Value: "x${@c}"},
				"c": {Value: "${@a | upper}"},
			}},
			wantErr: `placeholder templates refer to each other in a cycle: a -> b -> c -> a`,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			err := SetTemplates(tC.templates)
			assert.EqualError(t, err, tC.wantErr)
		})
	}
}

func TestSetTemplatesReportsAllErrors(t *testing.T) {
	err := SetTemplates(config.Templates{
		Filters:      map[string]config.FilterTemplate{"b": {Filters: "bad_b"}, "a": {Filters: "bad_a"}},
		Placeholders: map[string]config.PlaceholderTemplate{"c": {}},
	})

	// Errors are sorted by template name and kind, so they are reported deterministically
	assert.EqualError(t, err, `filter template "a": unknown filter "bad_a"
filter template "b": unknown filter "bad_b"
placeholder template "c": define either env or value`)
}

func TestSetTemplatesKeepsPreviousTemplatesOnError(t *testing.T) {
	setTemplates(t, jdbcTemplates)

	assert.Error(t, SetTemplates(config.Templates{Filters: map[string]config.FilterTemplate{"bad": {Filters: "bad"}}}))

	result, err := ProcessValue("${@db_host}")
	assert.NoError(t, err)
	assert.Equal(t, "localhost", result)
}

func TestSetTemplatesWithUnknownFiltersAllowed(t *testing.T) {
	filter.SetAllowUnknownFilters(true)
	t.Cleanup(func() { filter.SetAllowUnknownFilters(false) })

	// Unknown filters are allowed, unknown templates are not
	setTemplates(t, config.Templates{Filters: map[string]config.FilterTemplate{"lenient": {Filters: "trim | not_a_filter"}}})
	assert.Error(t, SetTemplates(config.Templates{Filters: map[string]config.FilterTemplate{"strict": {Filters: "@missing"}}}))

	t.Setenv("TPL_VALUE", " a ")
	result, err := ProcessValue("${TPL_VALUE | @lenient}")
	assert.NoError(t, err)
	assert.Equal(t, "a", result)
}

func TestChainedFilterTemplates(t *testing.T) {
	setTemplates(t, config.Templates{
		Filters: map[string]config.FilterTemplate{
			"clean":        {Filters: "trim | lower"},
			"encode":       {Filters: "base64"},
			"clean_encode": {Filters: "@clean | @encode"},
		},
		Placeholders: map[string]config.PlaceholderTemplate{
			"token": {Env: "TPL_TOKEN", Filters: "@clean | @encode | upper"},
		},
	})
	t.Setenv("TPL_TOKEN", " AbC ")

	testCases := []struct {
		desc  string
		value string
		want  string
	}{
		{desc: "in a placeholder", value: "${TPL_TOKEN | @clean | @encode}", want: "YWJj"},
		{desc: "in a filter template", value: "${TPL_TOKEN | @clean_encode}", want: "YWJj"},
		{desc: "in a placeholder template", value: "${@token}", want: "YWJJ"},
		{desc: "mixed with filters", value: "${TPL_TOKEN | @clean | upper | @encode}", want: "QUJD"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := ProcessValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

func TestValueTemplateWithEmptyPlaceholder(t *testing.T) {
	// An empty placeholder is kept as is, also within the value of a template
	setTemplates(t, config.Templates{Placeholders: map[string]config.PlaceholderTemplate{"literal": {Value: "a${}b"}}})

	result, err := ProcessValue("${@literal}")
	assert.NoError(t, err)
	assert.Equal(t, "a${}b", result)
}

// TestFailOnMissing checks which placeholders fail for missing variables when enabled, depending on the defaults they provide
func TestFailOnMissing(t *testing.T) {
	setTemplates(t, config.Templates{
		Filters: map[string]config.FilterTemplate{
			"fallback": {Filters: "trim | default(fallback)"},
			"clean":    {Filters: "trim"},
		},
		Placeholders: map[string]config.PlaceholderTemplate{
			"with_default":    {Env: "MISSING_TEST_VAR", Default: "template-default"},
			"with_filter":     {Env: "MISSING_TEST_VAR", Filters: "@fallback"},
			"without_default": {Env: "MISSING_TEST_VAR"},
			"url":             {Value: "http://${MISSING_TEST_VAR}/"},
		},
	})
	filter.SetFailOnMissing(true)
	t.Cleanup(func() { filter.SetFailOnMissing(false) })
	t.Setenv("EMPTY_TEST_VAR", "")

	testCases := []struct {
		desc    string
		value   string
		want    any
		wantErr string
	}{
		{desc: "missing variable", value: "${MISSING_TEST_VAR}", wantErr: `environment variable "MISSING_TEST_VAR" is not set`},
		{desc: "embedded missing variable", value: "a ${MISSING_TEST_VAR} b", wantErr: `environment variable "MISSING_TEST_VAR" is not set`},
		{desc: "empty variable", value: "${EMPTY_TEST_VAR}", want: ""},
		{desc: "default filter", value: "${MISSING_TEST_VAR | default(5432)}", want: "5432"},
		{desc: "default filter later in the chain", value: "${MISSING_TEST_VAR | trim | default(5432) | to_int}", want: 5432},
		{desc: "filter template with default", value: "${MISSING_TEST_VAR | @fallback}", want: "fallback"},
		{desc: "filter template without default", value: "${MISSING_TEST_VAR | @clean}", wantErr: "is not set"},
		{desc: "placeholder template with default", value: "${@with_default}", want: "template-default"},
		{desc: "placeholder template with default filter", value: "${@with_filter}", want: "fallback"},
		{desc: "placeholder template followed by default", value: "${@without_default | default(outer)}", want: "outer"},
		{desc: "placeholder template without default", value: "${@without_default}", wantErr: "is not set"},
		{desc: "value template with missing variable", value: "${@url}", wantErr: "is not set"},
		{desc: "required", value: "${EMPTY_TEST_VAR | required}", wantErr: "filter required: the value is empty or the environment variable is not set"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := ProcessValue(tC.value)
			if tC.wantErr != "" {
				assert.ErrorContains(t, err, tC.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

// TestTemplateReferences checks that placeholder and filter templates referenced by a value are listed once in order
func TestTemplateReferences(t *testing.T) {
	assert.Equal(t, []string{"@db_url", "@secret", "@clean"}, TemplateReferences("${@db_url}?p=${PASSWORD | @secret | upper | @clean} ${@db_url}"))
	assert.Empty(t, TemplateReferences("${PLAIN | trim} text"))
	assert.Empty(t, TemplateReferences("no placeholders"))
}
