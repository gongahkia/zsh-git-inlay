package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

const MaxConfigBytes = 1 << 20

// Diagnostic identifies the offending setting in both human and JSON output.
type Diagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

func (diagnostic *Diagnostic) Error() string {
	key := ""
	if diagnostic.Key != "" {
		key = " (" + diagnostic.Key + ")"
	}
	return fmt.Sprintf("%s:%d%s: %s", diagnostic.Path, diagnostic.Line, key, diagnostic.Message)
}

type field struct {
	key        string
	line       int
	start, end int // complete key/value expression, excluding trailing comment
	valueStart int
}

type document struct {
	fields map[string]field
	values map[string]string
	// Section ends allow insertion without duplicating an existing table.
	sections map[string]int
}

func parseDocument(content []byte, path string, allowed map[string]bool) (document, error) {
	doc := document{fields: map[string]field{}, values: map[string]string{}, sections: map[string]int{}}
	if len(content) > MaxConfigBytes {
		return doc, &Diagnostic{Path: path, Line: 1, Message: "configuration exceeds 1 MiB"}
	}
	// The stable decoder validates TOML semantics (including repeated tables,
	// duplicate dotted keys, escapes and types). The pinned syntax API is only
	// used to retain precise source spans for comment-preserving edits.
	var decoded map[string]any
	if err := toml.Unmarshal(content, &decoded); err != nil {
		line := 1
		var decodeError *toml.DecodeError
		if errors.As(err, &decodeError) {
			line, _ = decodeError.Position()
		}
		return doc, &Diagnostic{Path: path, Line: line, Message: err.Error()}
	}
	var parser unstable.Parser
	parser.Reset(content)
	section := ""
	for parser.NextExpression() {
		node := parser.Expression()
		if node.Kind == unstable.Comment {
			continue
		}
		keys := []string{}
		iterator := node.Key()
		lastKeyEnd := 0
		firstKeyStart := -1
		for iterator.Next() {
			part := iterator.Node()
			if firstKeyStart < 0 {
				firstKeyStart = int(part.Raw.Offset)
			}
			keys = append(keys, string(part.Data))
			lastKeyEnd = int(part.Raw.Offset + part.Raw.Length)
		}
		key := strings.Join(keys, ".")
		start, end := int(node.Raw.Offset), int(node.Raw.Offset+node.Raw.Length)
		if node.Kind == unstable.Table || node.Kind == unstable.ArrayTable {
			// Header nodes expose ranges on their keys, not on the parent.
			start = bytes.LastIndexByte(content[:firstKeyStart], '\n') + 1
		}
		line := bytes.Count(content[:start], []byte("\n")) + 1
		if node.Kind == unstable.Table {
			if section != "" {
				doc.sections[section] = start
			}
			section = key
			doc.sections[section] = len(content)
			// Reject unsupported tables even if they are empty. Repository
			// configuration can never grant a new provider/privacy capability.
			if allowed != nil {
				known := false
				for allowedKey := range allowed {
					known = known || strings.HasPrefix(allowedKey, section+".")
				}
				if !known {
					return doc, &Diagnostic{Path: path, Line: line, Key: section, Message: "unsupported section"}
				}
			}
			continue
		}
		if node.Kind != unstable.KeyValue {
			return doc, &Diagnostic{Path: path, Line: line, Key: key, Message: "array tables are not supported"}
		}
		if section != "" {
			key = section + "." + key
		}
		valueStart := lastKeyEnd + bytes.IndexByte(content[lastKeyEnd:end], '=') + 1
		for valueStart < end && (content[valueStart] == ' ' || content[valueStart] == '\t') {
			valueStart++
		}
		doc.fields[key] = field{key: key, line: line, start: start, end: end, valueStart: valueStart}
		if allowed != nil && !allowed[key] {
			return doc, &Diagnostic{Path: path, Line: line, Key: key, Message: "unsupported key"}
		}
		// Canonical scalar/array values let the schema validators operate on
		// decoded strings, rather than splitting on a '#' inside a binding.
		var single struct{ Value any }
		if err := toml.Unmarshal(append([]byte("Value = "), content[valueStart:end]...), &single); err != nil {
			return doc, &Diagnostic{Path: path, Line: line, Key: key, Message: err.Error()}
		}
		raw, err := canonical(single.Value)
		if err != nil {
			if allowed != nil {
				return doc, &Diagnostic{Path: path, Line: line, Key: key, Message: err.Error()}
			}
			raw = string(content[valueStart:end])
		}
		if allowed != nil {
			for _, spec := range append(Schema(false), Schema(true)...) {
				if spec.Key != key {
					continue
				}
				valid := false
				switch spec.Type {
				case "string", "duration":
					_, valid = single.Value.(string)
				case "boolean":
					_, valid = single.Value.(bool)
				case "integer":
					_, valid = single.Value.(int64)
				case "string array":
					_, valid = single.Value.([]any)
				}
				if !valid {
					return doc, &Diagnostic{Path: path, Line: line, Key: key, Message: "expected " + spec.Type}
				}
			}
		}
		doc.values[key] = raw
	}
	if err := parser.Error(); err != nil {
		return doc, &Diagnostic{Path: path, Line: 1, Message: err.Error()}
	}
	return doc, nil
}

func canonical(value any) (string, error) {
	switch value := value.(type) {
	case string:
		data, _ := json.Marshal(value)
		return string(data), nil
	case bool:
		return strconv.FormatBool(value), nil
	case int64:
		return strconv.FormatInt(value, 10), nil
	case []any:
		for _, item := range value {
			if _, ok := item.(string); !ok {
				return "", errors.New("expected a string array")
			}
		}
		data, _ := json.Marshal(value)
		return string(data), nil
	default:
		return "", errors.New("expected a string, integer, boolean, or string array")
	}
}

func parse(content string, allowed map[string]bool) (string, map[string]string, error) {
	doc, err := parseDocument([]byte(content), "config", allowed)
	return "", doc.values, err
}

func unquote(value string) string {
	var decoded struct{ V string }
	if err := toml.Unmarshal([]byte("V = "+value), &decoded); err == nil {
		return decoded.V
	}
	return value
}

func quoted(value string) bool {
	var decoded struct{ V string }
	return toml.Unmarshal([]byte("V = "+value), &decoded) == nil
}

func stringArray(value string) bool {
	var decoded struct{ V []string }
	return strings.HasPrefix(strings.TrimSpace(value), "[") && toml.Unmarshal([]byte("V = "+value), &decoded) == nil
}

func policyArray(value, key string) ([]string, error) {
	var decoded struct{ V []string }
	if !stringArray(value) || toml.Unmarshal([]byte("V = "+value), &decoded) != nil {
		return nil, fmt.Errorf("%s must be a string array", key)
	}
	seen := map[string]bool{}
	for _, item := range decoded.V {
		if item == "" || len(item) > 80 || seen[item] {
			return nil, fmt.Errorf("%s contains an empty, oversized, or duplicate item", key)
		}
		seen[item] = true
	}
	return decoded.V, nil
}

func withLocation(err error, content []byte, path string) error {
	if err == nil {
		return nil
	}
	var diagnostic *Diagnostic
	if errors.As(err, &diagnostic) {
		copy := *diagnostic
		copy.Path = path
		return &copy
	}
	doc, _ := parseDocument(content, path, nil)
	message := strings.TrimPrefix(err.Error(), "invalid config "+path+": ")
	message = strings.TrimPrefix(message, "invalid repository config "+path+": ")
	keys := make([]string, 0, len(doc.fields))
	for key := range doc.fields {
		keys = append(keys, key)
	}
	// Prefer the longest key when validation mentions related settings.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, key := range keys {
		if strings.Contains(err.Error(), key) {
			return &Diagnostic{Path: path, Line: doc.fields[key].line, Key: key, Message: message}
		}
	}
	// Cross-field requirements may refer to an absent setting. Attribute the
	// problem to the provider selection that made that setting mandatory.
	if item, ok := doc.fields["provider.name"]; ok && strings.Contains(err.Error(), "provider.") {
		return &Diagnostic{Path: path, Line: item.line, Key: item.key, Message: message}
	}
	return &Diagnostic{Path: path, Line: 1, Message: message}
}
