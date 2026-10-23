package common

import "encoding/json"

func Unmarshal[T any](data []byte) (T, error) {
	var result T
	err := json.Unmarshal(data, &result)
	if err != nil {
		return result, err
	}
	return result, nil
}

func Marshal[T any](value T) ([]byte, error) {
	return json.Marshal(value)
} 