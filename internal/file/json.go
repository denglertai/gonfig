package file

import (
	"fmt"
	"io"
	"iter"
	"strconv"

	"github.com/Jeffail/gabs/v2"
	"github.com/samber/lo"
)

// hierachicalConfigBase represents a base configuration entry
type hierachicalConfigBase interface {
	ConfigEntry
	isEdited() bool
}

// HierarchicalConfigEntry represents a single configuration entry
type HierarchicalConfigEntry struct {
	hierachicalConfigBase
	edited        bool
	originalValue interface{}
	value         string
	path          string
	key           string
	hierarchy     []string
}

// Key returns the key of the configuration entry
func (j *HierarchicalConfigEntry) Key() string {
	return j.key
}

// Path returns the path of the configuration entry
func (j *HierarchicalConfigEntry) Path() string {
	return j.path
}

// GetValue returns the value of the configuration entry
func (j *HierarchicalConfigEntry) GetValue() string {
	return j.value
}

// SetValue sets the value of the configuration entry
func (j *HierarchicalConfigEntry) SetValue(value string) {
	j.edited = j.edited || j.value != value
	j.value = value
}

func (j *HierarchicalConfigEntry) getConvertedValue() (interface{}, error) {
	// Convert the value to the original type and return it
	switch j.originalValue.(type) {
	case int:
		return strconv.Atoi(j.value)
	case float64:
		return strconv.ParseFloat(j.value, 64)
	case string:
		return j.value, nil
	case bool:
		return strconv.ParseBool(j.value)
	}

	return nil, fmt.Errorf("unsupported type: %T", j.originalValue)
}

// isEdited returns whether the configuration entry has been edited
func (j *HierarchicalConfigEntry) isEdited() bool {
	return j.edited
}

// HierarchicalConfigKey represents a single configuration entry's key
type HierarchicalConfigKey struct {
	hierachicalConfigBase
	edited        bool
	originalValue interface{}
	value         string
	path          string
	key           string
	hierarchy     []string
}

// Key returns the key of the configuration entry
func (j *HierarchicalConfigKey) Key() string {
	return j.key
}

// Path returns the path of the configuration entry
func (j *HierarchicalConfigKey) Path() string {
	return j.path
}

// GetValue returns the value of the configuration entry
func (j *HierarchicalConfigKey) GetValue() string {
	return j.value
}

// SetValue sets the value of the configuration entry
func (j *HierarchicalConfigKey) SetValue(value string) {
	j.edited = j.edited || j.value != value
	j.value = value
}

// isEdited returns whether the configuration entry has been edited
func (j *HierarchicalConfigKey) isEdited() bool {
	return j.edited
}

// hierarchicalConfigHandler represents a basic configuration handler for hierarchical configuration files
type hierarchicalConfigHandler struct {
	entries []ConfigEntry
}

// JsonConfigFileHandler represents a configuration file handler
type JsonConfigFileHandler struct {
	hierarchicalConfigHandler
	container *gabs.Container
}

// NewJsonConfigFileHandler creates a new JSON configuration file handler
func NewJsonConfigFileHandler() *JsonConfigFileHandler {
	return &JsonConfigFileHandler{
		hierarchicalConfigHandler: hierarchicalConfigHandler{
			entries: make([]ConfigEntry, 0),
		},
	}
}

// Read reads the configuration file
func (j *JsonConfigFileHandler) Read(source io.Reader) (err error) {
	j.container, err = gabs.ParseJSONBuffer(source)
	if err != nil {
		return err
	}
	err = j.handleChildren(j.container, "", []string{})
	return err
}

// Process processes the configuration file and returns the configuration entries
func (j *JsonConfigFileHandler) Process() (iter.Seq[ConfigEntry], error) {
	return func(yield func(ConfigEntry) bool) {
		for _, entry := range j.entries {
			if !yield(entry) {
				break
			}
		}
	}, nil
}

func (j *JsonConfigFileHandler) handleChildren(container *gabs.Container, path string, hierarchy []string) error {
	childrenMap := container.ChildrenMap()
	// In case the container is not a map the result length will be 0
	if len(childrenMap) > 0 {
		for key, value := range childrenMap {
			currentPath := appendToPath(path, key)
			currentHierarchy := append(hierarchy, key)
			data := value.Data()
			switch v := data.(type) {
			case int:
				j.appendEntry(currentPath, key, currentHierarchy, v)
				continue
			case float64:
				j.appendEntry(currentPath, key, currentHierarchy, v)
				continue
			case string:
				j.appendEntry(currentPath, key, currentHierarchy, v)
				continue
			}

			// If the value is a container, we need to process it as well
			if value != nil {
				if err := j.handleChildren(value, currentPath, currentHierarchy); err != nil {
					return err
				}
			}
		}
		return nil
	}

	children := container.Children()
	for i, child := range children {
		currentPath := appendToPath(path, fmt.Sprint(i))
		currentHierarchy := append(hierarchy, fmt.Sprint(i))
		if err := j.handleChildren(child, currentPath, currentHierarchy); err != nil {
		}
	}

	// Must be a value
	if len(children) == 0 {
		copiedHierarchy := append(make([]string, 0), hierarchy...)
		j.appendEntry(path, copiedHierarchy[len(copiedHierarchy)-1], copiedHierarchy, container.Data())
	}

	return nil
}

func (j *hierarchicalConfigHandler) appendEntry(path, key string, hierarchy []string, value interface{}) {
	entry := &HierarchicalConfigEntry{
		path:          path,
		key:           key,
		originalValue: value,
		value:         fmt.Sprintf("%v", value),
		hierarchy:     hierarchy,
	}

	j.entries = append(j.entries, entry)
}

func (j *hierarchicalConfigHandler) appendKey(path, key string, hierarchy []string, value interface{}) {
	entry := &HierarchicalConfigKey{
		path:          path,
		key:           key,
		originalValue: value,
		value:         fmt.Sprintf("%v", value),
		hierarchy:     hierarchy,
	}

	j.entries = append(j.entries, entry)
}

func appendToPath(path string, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// Write writes the configuration entries to the target
func (j *JsonConfigFileHandler) Write(target io.Writer) error {
	filteredEntries := lo.FilterMap(j.entries, func(entry ConfigEntry, _ int) (*HierarchicalConfigEntry, bool) {
		jce := entry.(*HierarchicalConfigEntry)
		return jce, jce.edited
	})

	for _, entry := range filteredEntries {
		val, err := entry.getConvertedValue()
		if err != nil {
			return err
		}

		_, err = j.container.Set(val, entry.hierarchy...)
		if err != nil {
			return err
		}
	}

	data := j.container.EncodeJSON(gabs.EncodeOptIndent("", "  "))
	_, err := target.Write(data)
	return err
}
