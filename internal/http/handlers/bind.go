package handlers

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// decodeMap reads the body as a generic JSON object for dual snake/camel binding.
func decodeMap(r io.Reader) (map[string]interface{}, error) {
	var m map[string]interface{}
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	return m, nil
}

func mapString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if t != "" {
					return t
				}
			case json.Number:
				return t.String()
			}
		}
	}
	return ""
}

func mapFloat(m map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case float64:
				return t
			case float32:
				return float64(t)
			case int:
				return float64(t)
			case int64:
				return float64(t)
			case json.Number:
				f, _ := t.Float64()
				return f
			case string:
				f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
				if err == nil {
					return f
				}
			}
		}
	}
	return 0
}

func mapBool(m map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case bool:
				return t
			case string:
				return t == "true" || t == "1"
			}
		}
	}
	return false
}
