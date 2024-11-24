package file

import (
	"fmt"
	"io"
	"iter"

	"github.com/Jeffail/gabs/v2"
	"github.com/samber/lo"
)

// JsonConfigEntry represents a single configuration entry
type JsonConfigEntry struct {
	originalValue interface{}
	value         string
	edited        bool
	path          string
	key           string
	hierarchy     []string
}

// Key returns the key of the configuration entry
func (j *JsonConfigEntry) Key() string {
	return j.key
}

// GetValue returns the value of the configuration entry
func (j *JsonConfigEntry) GetValue() string {
	return j.value
}

// SetValue sets the value of the configuration entry
func (j *JsonConfigEntry) SetValue(value string) {
	j.value = value
	j.edited = true
}

// JsonConfigFileHandler represents a configuration file handler
type JsonConfigFileHandler struct {
	container *gabs.Container
	entries   []ConfigEntry
}

// NewXmlConfigFileHandler creates a new XML configuration file handler
func NewJsonConfigFileHandler() *JsonConfigFileHandler {
	return &JsonConfigFileHandler{
		entries: make([]ConfigEntry, 0),
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
		j.appendEntry(path, hierarchy[len(hierarchy)-1], hierarchy, container.Data())
	}

	return nil
}

func (j *JsonConfigFileHandler) appendEntry(path, key string, hierarchy []string, value interface{}) {
	entry := &JsonConfigEntry{
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
	filteredEntries := lo.FilterMap(j.entries, func(entry ConfigEntry, _ int) (*JsonConfigEntry, bool) {
		jce := entry.(*JsonConfigEntry)
		return jce, jce.edited
	})

	for _, entry := range filteredEntries {
		_, err := j.container.Set(entry.value, entry.hierarchy...)
		if err != nil {
			return err
		}
	}

	data := j.container.EncodeJSON(gabs.EncodeOptIndent("", "  "))
	_, err := target.Write(data)
	return err
}
