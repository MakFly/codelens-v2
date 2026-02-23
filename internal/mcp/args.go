package mcp

import (
	"fmt"
	"strings"

	gmc "github.com/mark3labs/mcp-go/mcp"
)

func argString(req gmc.CallToolRequest, key string, required bool) (string, error) {
	raw, ok := req.Params.Arguments[key]
	if !ok {
		if required {
			return "", invalidInputf("missing required argument %q", key)
		}
		return "", nil
	}
	v, ok := raw.(string)
	if !ok {
		return "", invalidInputf("argument %q must be a string", key)
	}
	v = strings.TrimSpace(v)
	if required && v == "" {
		return "", invalidInputf("argument %q cannot be empty", key)
	}
	return v, nil
}

func argFloat(req gmc.CallToolRequest, key string) (float64, bool, error) {
	raw, ok := req.Params.Arguments[key]
	if !ok {
		return 0, false, nil
	}
	switch v := raw.(type) {
	case float64:
		return v, true, nil
	case float32:
		return float64(v), true, nil
	case int:
		return float64(v), true, nil
	case int64:
		return float64(v), true, nil
	default:
		return 0, false, invalidInputf("argument %q must be numeric", key)
	}
}

func argArray(req gmc.CallToolRequest, key string, required bool) ([]interface{}, error) {
	raw, ok := req.Params.Arguments[key]
	if !ok {
		if required {
			return nil, invalidInputf("missing required argument %q", key)
		}
		return nil, nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, invalidInputf("argument %q must be an array", key)
	}
	if required && len(arr) == 0 {
		return nil, invalidInputf("argument %q must contain at least one element", key)
	}
	return arr, nil
}

func argObject(raw interface{}, key string) (map[string]interface{}, error) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, invalidInputf("element in %q must be an object", key)
	}
	return m, nil
}

func toIntField(m map[string]interface{}, field string) (int, error) {
	v, ok := m[field]
	if !ok {
		return 0, invalidInputf("missing field %q", field)
	}
	switch x := v.(type) {
	case float64:
		return int(x), nil
	case float32:
		return int(x), nil
	case int:
		return x, nil
	case int64:
		return int(x), nil
	default:
		return 0, invalidInputf("field %q must be numeric", field)
	}
}

func requiredStringField(m map[string]interface{}, field string) (string, error) {
	v, ok := m[field]
	if !ok {
		return "", invalidInputf("missing field %q", field)
	}
	s, ok := v.(string)
	if !ok {
		return "", invalidInputf("field %q must be a string", field)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", invalidInputf("field %q cannot be empty", field)
	}
	return s, nil
}

func argDebug(req gmc.CallToolRequest) string {
	return fmt.Sprintf("args=%v", req.Params.Arguments)
}
