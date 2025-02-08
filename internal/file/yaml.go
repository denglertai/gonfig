package file

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"slices"
	"strconv"

	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
)

// Set sets a value in the hierarchical container
func setHierarchical(container map[string]interface{}, value interface{}, hierarchy ...string) error {
	if len(hierarchy) == 0 {
		return fmt.Errorf("empty hierarchy")
	}

	// Hierarchy at the first level will always be a string
	currentLocation := container[hierarchy[0]]
	remainingHierarchy := hierarchy[1:]

	return setInner(currentLocation, value, remainingHierarchy)
}

func setInner(location interface{}, value interface{}, hierarchy []string) error {
	if len(hierarchy) == 0 {
		return fmt.Errorf("empty hierarchy")
	}

	switch l := location.(type) {
	case nil:
		// If the current location is nil we cannot continue
		return fmt.Errorf("nil hierarchy")
	case map[string]interface{}:
		if len(hierarchy) == 1 {
			// We are at the end of the hierarchy, we can set the value
			l[hierarchy[0]] = value
			return nil
		}

		// If the current location is a map, we need to go deeper
		currentLocation := l[hierarchy[0]]
		remainingHierarchy := hierarchy[1:]
		err := setInner(currentLocation, value, remainingHierarchy)
		if err != nil {
			return err
		}
		l[hierarchy[0]] = currentLocation
	case []interface{}:
		// If the current location is a list, we need to go deeper
		if len(hierarchy) == 0 {
			return fmt.Errorf("empty hierarchy at slice level")
		}
		// if this a slice, we expected the current entry to be an integer
		index, err := strconv.Atoi(hierarchy[0])
		if err != nil {
			return err
		}

		if len(hierarchy) == 1 {
			// We are at the end of the hierarchy, we can set the value
			l[index] = value
			return nil
		}

		// If the current location is a map, we need to go deeper
		currentLocation := l[index]
		remainingHierarchy := hierarchy[1:]
		err = setInner(currentLocation, value, remainingHierarchy)
		if err != nil {
			return err
		}
		l[index] = currentLocation
	default:
		return fmt.Errorf("unsupported type: %T", l)
	}

	return nil
}

// moveYaml moves a value from one location to another
func moveYaml(container map[string]interface{}, from []string, newKey string) error {
	// If the from or the newKey is empty we cannot continue
	if len(from) == 0 || len(newKey) == 0 {
		return nil
	}

	return moveYamlInner(container, from, newKey)
}

func moveYamlInner(location interface{}, remainingHierachy []string, newKey string) error {
	if len(remainingHierachy) == 0 {
		return fmt.Errorf("empty hierarchy")
	}

	switch l := location.(type) {
	case nil:
		// If the current location is nil we cannot continue
		return fmt.Errorf("nil hierarchy")
	case map[string]interface{}:
		if len(remainingHierachy) == 1 {
			// We are at the end of the hierarchy, we can set the value
			l[newKey] = l[remainingHierachy[0]]
			delete(l, remainingHierachy[0])
			return nil
		}

		// If the current location is a map, we need to go deeper
		currentLocation := l[remainingHierachy[0]]
		remainingHierarchy := remainingHierachy[1:]
		err := moveYamlInner(currentLocation, remainingHierarchy, newKey)
		if err != nil {
			return err
		}
		l[remainingHierachy[0]] = currentLocation
	case []interface{}:
		// If the current location is a list, we need to go deeper
		if len(remainingHierachy) == 0 {
			return fmt.Errorf("empty hierarchy at slice level")
		}
		// if this a slice, we expected the current entry to be an integer
		index, err := strconv.Atoi(remainingHierachy[0])
		if err != nil {
			return err
		}

		if len(remainingHierachy) == 1 {
			// We are at the end of the hierarchy, we can set the value
			// l[newKey] = l[index]
			return nil
		}

		// If the current location is a map, we need to go deeper
		currentLocation := l[index]
		remainingHierarchy := remainingHierachy[1:]
		err = moveYamlInner(currentLocation, remainingHierarchy, newKey)
		if err != nil {
			return err
		}
		l[index] = currentLocation
	default:
		return fmt.Errorf("unsupported type: %T", l)
	}

	return nil
}

// YamlConfigFileHandler represents a configuration file handler
type YamlConfigFileHandler struct {
	hierarchicalConfigHandler
	container map[string]interface{}
}

// NewYamlConfigFileHandler creates a new YAML configuration file handler
func NewYamlConfigFileHandler() *YamlConfigFileHandler {
	return &YamlConfigFileHandler{
		hierarchicalConfigHandler: hierarchicalConfigHandler{
			entries: make([]ConfigEntry, 0),
		},
		container: make(map[string]interface{}),
	}
}

// Read reads the configuration file
func (y *YamlConfigFileHandler) Read(source io.Reader) (err error) {
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(source)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(buf.Bytes(), &y.container)
	if err != nil {
		return err
	}
	err = y.handleChildren(y.container, "", []string{})

	return err
}

func (y *YamlConfigFileHandler) handleChildren(container map[string]interface{}, path string, hierarchy []string) error {
	for key, value := range container {
		currentPath := appendToPath(path, key)
		copiedHierarchy := append(make([]string, 0), hierarchy...)
		currentHierarchy := append(copiedHierarchy, key)
		// Append currenty key as entry as well
		y.appendKey(currentPath, key, currentHierarchy, key)
		err := y.handleEntry(currentPath, key, currentHierarchy, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func (y *YamlConfigFileHandler) handleEntry(path string, key string, hierarchy []string, value interface{}) error {
	switch v := value.(type) {
	case int:
		y.appendEntry(path, key, hierarchy, v)
	case float64:
		y.appendEntry(path, key, hierarchy, v)
	case string:
		y.appendEntry(path, key, hierarchy, v)
	case bool:
		y.appendEntry(path, key, hierarchy, v)
	case map[interface{}]interface{}:
		// Convert the map to a map[string]interface{}
		m := make(map[string]interface{})
		for k, v := range v {
			m[fmt.Sprintf("%v", k)] = v
		}
		// Deeper down the rabbit hole
		err := y.handleChildren(m, path, hierarchy)
		if err != nil {
			return err
		}
	case map[string]interface{}:
		// Deeper down the rabbit hole
		err := y.handleChildren(v, path, hierarchy)
		if err != nil {
			return err
		}
	case []interface{}:
		for i, item := range v {
			is := strconv.Itoa(i)
			currentPath := appendToPath(path, is)
			copiedHierarchy := append(make([]string, 0), hierarchy...)
			currentHierarchy := append(copiedHierarchy, is)
			err := y.handleEntry(currentPath, is, currentHierarchy, item)
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported type: %T", v)
	}
	return nil
}

// Process processes the configuration file and returns the configuration entries
func (y *YamlConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return func(yield func(ConfigEntry) bool) {
		for _, entry := range y.entries {
			if !yield(entry) {
				break
			}
		}
	}, nil
}

// Write writes the configuration entries to the target
func (y *YamlConfigFileHandler) Write(target io.Writer) error {
	filteredEntries := lo.FilterMap(y.entries, func(entry ConfigEntry, _ int) (hierachicalConfigBase, bool) {
		hce := entry.(hierachicalConfigBase)

		return hce, hce.isEdited()
	})

	for _, entry := range filteredEntries {
		hce, isHce := entry.(*HierarchicalConfigEntry)
		if isHce {
			val, err := hce.getConvertedValue()
			if err != nil {
				return err
			}

			err = setHierarchical(y.container, val, hce.hierarchy...)
			if err != nil {
				return err
			}

			// We have processed the entry, we can
			continue
		}

		hck, isHck := entry.(*HierarchicalConfigEntry)
		if isHck {
			err := setHierarchical(y.container, hck.key, hck.hierarchy...)
			if err != nil {
				return err
			}
		}
	}

	hcks := lo.Filter(y.entries, func(entry ConfigEntry, _ int) bool {
		hck, isHck := entry.(*HierarchicalConfigKey)
		return isHck && hck.isEdited()
	})

	// Sort the entries by hierarchy length so that we can move the deeper nested entries first
	slices.SortFunc(hcks, func(i, j ConfigEntry) int {
		lenI := len(i.(*HierarchicalConfigKey).hierarchy)
		lenJ := len(j.(*HierarchicalConfigKey).hierarchy)
		if lenI < lenJ {
			return 1
		} else if lenI > lenJ {
			return -1
		}
		return 0
	})

	for _, entry := range hcks {
		hck := entry.(*HierarchicalConfigKey)
		err := moveYaml(y.container, hck.hierarchy, hck.value)
		if err != nil {
			return err
		}
	}

	buf := bytes.Buffer{}
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	err := enc.Encode(y.container)
	if err != nil {
		return err
	}

	_, err = target.Write(buf.Bytes())

	return err
}
