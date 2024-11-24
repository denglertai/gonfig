package file

import (
	"bytes"
	"fmt"
	"io"
	"iter"
	"strconv"

	"github.com/samber/lo"
	"gopkg.in/yaml.v2"
)

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
		currentHierarchy := append(hierarchy, key)
		switch v := value.(type) {
		case int:
			y.appendEntry(currentPath, key, currentHierarchy, v)
			continue
		case float64:
			y.appendEntry(currentPath, key, currentHierarchy, v)
			continue
		case string:
			y.appendEntry(currentPath, key, currentHierarchy, v)
			continue
		case map[interface{}]interface{}:
			// Convert the map to a map[string]interface{}
			m := make(map[string]interface{})
			for k, v := range v {
				m[fmt.Sprintf("%v", k)] = v
			}
			// Deeper down the rabbit hole
			err := y.handleChildren(m, currentPath, currentHierarchy)
			if err != nil {
				return err
			}
		case []interface{}:
			for i, item := range v {
				is := strconv.Itoa(i)

				currentPath := appendToPath(currentPath, is)
				currentHierarchy := append(currentHierarchy, is)
				switch iv := item.(type) {
				case int:
					y.appendEntry(currentPath, is, currentHierarchy, iv)
					continue
				case float64:
					y.appendEntry(currentPath, is, currentHierarchy, iv)
					continue
				case string:
					y.appendEntry(currentPath, is, currentHierarchy, iv)
					continue
				case map[interface{}]interface{}:
					// Convert the map to a map[string]interface{}
					miv := make(map[string]interface{})
					for k, v := range v {
						miv[fmt.Sprintf("%v", k)] = v
					}
					// Deeper down the rabbit hole
					err := y.handleChildren(miv, currentPath, currentHierarchy)
					if err != nil {
						return err
					}
				}
			}
		}
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
	filteredEntries := lo.FilterMap(y.entries, func(entry ConfigEntry, _ int) (*HierarchicalConfigEntry, bool) {
		jce := entry.(*HierarchicalConfigEntry)
		return jce, jce.edited
	})

	for _, entry := range filteredEntries {
		val, err := entry.getConvertedValue()
		if err != nil {
			return err
		}

		fmt.Print(val)

		// _, err = y.container.Set(val, entry.hierarchy...)
		if err != nil {
			return err
		}
	}

	data, err := yaml.Marshal(y.container)
	if err != nil {
		return err
	}

	_, err = target.Write(data)

	return err
}
