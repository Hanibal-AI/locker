package pii

import "encoding/json"

// MaskJSON walks every string value in a JSON document (request or
// response body) and masks any PII found in it, recording original values
// in table. If nothing was found, the original bytes are returned
// unchanged so a request with no PII is forwarded byte-for-byte.
func MaskJSON(body []byte, engine *Engine, table *Table) ([]byte, error) {
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	before := table.Len()
	masked := walkStrings(data, func(s string) string {
		matches := engine.Detect(s)
		if len(matches) == 0 {
			return s
		}
		return table.Mask(s, matches)
	})
	if table.Len() == before {
		return body, nil
	}
	return json.Marshal(masked)
}

// UnmaskJSON walks every string value in a JSON document and restores any
// placeholder tokens back to their original values using table. If table
// is empty, the original bytes are returned unchanged.
func UnmaskJSON(body []byte, table *Table) ([]byte, error) {
	if table.Len() == 0 {
		return body, nil
	}

	var data any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	restored := walkStrings(data, table.Unmask)
	return json.Marshal(restored)
}

func walkStrings(v any, fn func(string) string) any {
	switch val := v.(type) {
	case string:
		return fn(val)
	case map[string]any:
		for k, vv := range val {
			val[k] = walkStrings(vv, fn)
		}
		return val
	case []any:
		for i, vv := range val {
			val[i] = walkStrings(vv, fn)
		}
		return val
	default:
		return v
	}
}
