package models

import (
	"database/sql/driver"
	"errors"

	"agent-task-manager/internal/common"
)

// StringArray represents a JSONB-serializable array of strings.
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	return common.Marshal(a)
}

func (a *StringArray) Scan(value any) error {
	if value == nil {
		*a = []string{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("unsupported scan type for StringArray")
	}
	arr, err := common.Unmarshal[StringArray](bytes)
	if err != nil {
		return err
	}
	*a = arr
	return nil
}

// JSONMap represents a JSONB-serializable map of key-value configurations.
type JSONMap map[string]any

func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	return common.Marshal(m)
}

func (m *JSONMap) Scan(value any) error {
	if value == nil {
		*m = JSONMap{}
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("unsupported scan type for JSONMap")
	}
	res, err := common.Unmarshal[JSONMap](bytes)
	if err != nil {
		return err
	}
	*m = res
	return nil
}

// ConfigValue returns m[key] as T. It reports false when the key is absent,
// the map is nil, or the stored value is not a T (the zero T is returned).
func ConfigValue[T any](m JSONMap, key string) (T, bool) {
	v, ok := m[key].(T)
	return v, ok
}
