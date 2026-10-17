package value

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/denglertai/gonfig/internal/config"
	"github.com/denglertai/gonfig/internal/filter"
)

// Templates are defined in the config file and referenced with a leading "@":
//   - filter templates are reusable filter chains, used as ${VAR | @name}
//   - placeholder templates are reusable placeholders, used as ${@name}, which read either an environment
//     variable with an optional default or a value containing placeholders, e.g. to build URLs
//
// They are set and validated once on startup by SetTemplates, so references within the templates are known to be valid.
var (
	filterTemplates      map[string]config.FilterTemplate
	placeholderTemplates map[string]config.PlaceholderTemplate
)

// templateNameRe matches valid template names. Viper converts the keys of the config file to lower case anyway.
var templateNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// templateName returns the name of the template referenced by "@name"
func templateName(name string) (string, bool) {
	return strings.CutPrefix(name, "@")
}

// SetTemplates validates the templates and makes them available to placeholders.
// The filters, including the ones of plugins, have to be registered beforehand, so unknown filters are detected.
func SetTemplates(templates config.Templates) error {
	if err := validateTemplates(templates); err != nil {
		return err
	}

	filterTemplates = templates.Filters
	placeholderTemplates = templates.Placeholders
	return nil
}

// filterTemplateFilter creates the filter applying the filter template
func filterTemplateFilter(name string) filter.Filter {
	template, found := filterTemplates[name]
	if !found {
		return filter.NewErrorFilter(fmt.Errorf("unknown filter template %q", name))
	}

	return filter.NewChainFilter(chainFilters(scanChain(template.Filters, false)))
}

// placeholderTemplateFilter creates the filter returning the value of the placeholder template
func placeholderTemplateFilter(name string) filter.Filter {
	template, found := placeholderTemplates[name]
	if !found {
		return filter.NewErrorFilter(fmt.Errorf("unknown placeholder template %q", name))
	}

	var filters []filter.Filter
	if template.Env != "" {
		filters = append(filters, filter.NewEnvVarFilter(template.Env))
		if template.Default != "" {
			filters = append(filters, filter.NewDefaultValueFilter(template.Default))
		}
		// A default may refer to a file as well
		filters = append(filters, filter.NewFileInterceptorFilter())
	} else {
		filters = append(filters, valueTemplateFilter{value: template.Value})
	}

	return filter.NewChainFilter(append(filters, chainFilters(scanChain(template.Filters, false))...))
}

// valueTemplateFilter returns the processed value of a value-based placeholder template
type valueTemplateFilter struct {
	value string
}

// Process processes the value of the template like any config value, ignoring its input
func (f valueTemplateFilter) Process(_ any) (any, error) {
	return ProcessValue(f.value)
}

// templateGraph holds the references between templates of the same kind, to detect cycles
type templateGraph map[string][]string

// validateTemplates checks the templates for invalid names and definitions, unknown filters and references, and cycles
func validateTemplates(templates config.Templates) error {
	var errs []error
	fail := func(kind string, name string, format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s template %q: %s", kind, name, fmt.Sprintf(format, args...)))
	}

	filterRefs := templateGraph{}
	for _, name := range sortedKeys(templates.Filters) {
		template := templates.Filters[name]

		if !templateNameRe.MatchString(name) {
			fail("filter", name, "invalid name, use lower case letters, digits and underscores")
		}
		if _, found := templates.Placeholders[name]; found {
			fail("filter", name, "the name is also used by a placeholder template")
		}
		if strings.TrimSpace(template.Filters) == "" {
			fail("filter", name, "filters must not be empty")
		}

		refs, chainErrs := validateChain(scanChain(template.Filters, false), templates)
		filterRefs[name] = refs
		for _, err := range chainErrs {
			fail("filter", name, "%v", err)
		}
	}

	placeholderRefs := templateGraph{}
	for _, name := range sortedKeys(templates.Placeholders) {
		template := templates.Placeholders[name]

		if !templateNameRe.MatchString(name) {
			fail("placeholder", name, "invalid name, use lower case letters, digits and underscores")
		}

		switch {
		case template.Env != "" && template.Value != "":
			fail("placeholder", name, "define either env or value, not both")
		case template.Env == "" && template.Value == "":
			fail("placeholder", name, "define either env or value")
		case template.Value != "" && template.Default != "":
			fail("placeholder", name, "default can only be used with env")
		}

		_, chainErrs := validateChain(scanChain(template.Filters, false), templates)
		for _, err := range chainErrs {
			fail("placeholder", name, "%v", err)
		}

		// The placeholders within the value may refer to other placeholder templates and use filter templates
		for _, p := range findPlaceholders(template.Value) {
			steps := scanChain(p.content(), true)
			if len(steps) == 0 {
				continue
			}

			if ref, isTemplate := templateName(steps[0].name); isTemplate {
				if _, found := templates.Placeholders[ref]; found {
					placeholderRefs[name] = append(placeholderRefs[name], ref)
				} else {
					fail("placeholder", name, "unknown placeholder template %q in value", ref)
				}
			}

			_, chainErrs := validateChain(steps[1:], templates)
			for _, err := range chainErrs {
				fail("placeholder", name, "%v in value", err)
			}
		}
	}

	if cycle := findCycle(filterRefs); cycle != nil {
		errs = append(errs, fmt.Errorf("filter templates refer to each other in a cycle: %s", strings.Join(cycle, " -> ")))
	}
	if cycle := findCycle(placeholderRefs); cycle != nil {
		errs = append(errs, fmt.Errorf("placeholder templates refer to each other in a cycle: %s", strings.Join(cycle, " -> ")))
	}

	return errors.Join(errs...)
}

// validateChain checks the filters and filter templates of a filter chain and returns the referenced filter templates
func validateChain(steps []chainStep, templates config.Templates) ([]string, []error) {
	var refs []string
	var errs []error

	for _, step := range steps {
		ref, isTemplate := templateName(step.name)
		switch {
		case isTemplate:
			if _, found := templates.Filters[ref]; found {
				refs = append(refs, ref)
			} else if _, found := templates.Placeholders[ref]; found {
				errs = append(errs, fmt.Errorf("%q is a placeholder template, which can only be used as ${@%s}", ref, ref))
			} else {
				errs = append(errs, fmt.Errorf("unknown filter template %q", ref))
			}
		case !filter.IsKnown(step.name) && !filter.UnknownFiltersAllowed():
			errs = append(errs, fmt.Errorf("unknown filter %q", step.name))
		}
	}

	return refs, errs
}

// findCycle returns the templates forming a cycle, e.g. [a b a], or nil if there is none
func findCycle(graph templateGraph) []string {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := map[string]int{}
	var path []string

	var visit func(name string) []string
	visit = func(name string) []string {
		switch state[name] {
		case visiting:
			// The cycle starts where the name was first visited
			start := slices.Index(path, name)
			return append(slices.Clone(path[start:]), name)
		case visited:
			return nil
		}

		state[name] = visiting
		path = append(path, name)
		for _, ref := range graph[name] {
			if cycle := visit(ref); cycle != nil {
				return cycle
			}
		}
		path = path[:len(path)-1]
		state[name] = visited
		return nil
	}

	for _, name := range sortedKeys(graph) {
		if cycle := visit(name); cycle != nil {
			return cycle
		}
	}
	return nil
}

// sortedKeys returns the keys of the map in sorted order, so errors are reported deterministically
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
