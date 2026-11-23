package esclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/searchboard/searchboard/internal/domain"
)

// Limits keep the schema of a huge cluster small enough for an editor.
const (
	maxSchemaIndices          = 2000
	maxSchemaFieldsPerIndex   = 2000
	maxSchemaFieldsTotal      = 50000
	maxMappingDepth           = 20
	mappingFilterPath         = "*.mappings"
	hiddenIndexPrefix         = "."
	propertiesKey             = "properties"
	defaultTypeForUntypedLeaf = "object"
)

// Schema returns the indices, aliases and mapping fields of the cluster.
//
// It handles both mapping layouts: typeless mappings (Elasticsearch 7+ and
// OpenSearch 2+) and mappings wrapped in a type name (Elasticsearch 5 and 6 and,
// depending on include_type_name, 7). Hidden indices (names starting with a dot)
// are listed after the regular ones so that limits cut them first.
func (c *Client) Schema(ctx context.Context) (domain.Schema, error) {
	var (
		mappings map[string]struct {
			Mappings map[string]json.RawMessage `json:"mappings"`
		}
		aliases map[string]struct {
			Aliases map[string]json.RawMessage `json:"aliases"`
		}
		wg   sync.WaitGroup
		errs [2]error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		query := url.Values{"filter_path": {mappingFilterPath}, "expand_wildcards": {"open"}}
		errs[0] = c.doJSON(ctx, http.MethodGet, "_mapping", query, nil, &mappings)
	}()
	go func() {
		defer wg.Done()
		query := url.Values{"filter_path": {"*.aliases"}, "expand_wildcards": {"open"}}
		errs[1] = c.doJSON(ctx, http.MethodGet, "_alias", query, nil, &aliases)
	}()
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return domain.Schema{}, err
		}
	}

	names := make([]string, 0, len(mappings))
	for name := range mappings {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		hiddenI, hiddenJ := strings.HasPrefix(names[i], hiddenIndexPrefix), strings.HasPrefix(names[j], hiddenIndexPrefix)
		if hiddenI != hiddenJ {
			return !hiddenI
		}
		return names[i] < names[j]
	})

	schema := domain.Schema{Indices: []domain.SchemaIndex{}}
	totalFields := 0
	for _, name := range names {
		if len(schema.Indices) >= maxSchemaIndices || totalFields >= maxSchemaFieldsTotal {
			schema.Truncated = true
			break
		}

		fields, cut, err := flattenMappings(mappings[name].Mappings)
		if err != nil {
			return domain.Schema{}, fmt.Errorf("esclient: mapping of index %q: %w", name, err)
		}
		if remaining := maxSchemaFieldsTotal - totalFields; len(fields) > remaining {
			fields, cut = fields[:remaining], true
		}
		totalFields += len(fields)
		schema.Truncated = schema.Truncated || cut

		index := domain.SchemaIndex{Name: name, Aliases: []string{}, Fields: fields}
		for alias := range aliases[name].Aliases {
			index.Aliases = append(index.Aliases, alias)
		}
		sort.Strings(index.Aliases)
		schema.Indices = append(schema.Indices, index)
	}
	return schema, nil
}

// flattenMappings turns a mappings object into sorted, de-duplicated fields.
// It reports whether the per-index limit cut the list short.
func flattenMappings(mappings map[string]json.RawMessage) ([]domain.SchemaField, bool, error) {
	seen := map[string]string{}

	if raw, ok := mappings[propertiesKey]; ok { // typeless layout
		if err := walkProperties(raw, "", 0, seen); err != nil {
			return nil, false, err
		}
	} else { // typed layout: {"<type>": {"properties": {...}}}
		for _, raw := range mappings {
			var typed map[string]json.RawMessage
			if json.Unmarshal(raw, &typed) != nil {
				continue // not a type mapping, such as "dynamic": "strict"
			}
			if props, ok := typed[propertiesKey]; ok {
				if err := walkProperties(props, "", 0, seen); err != nil {
					return nil, false, err
				}
			}
		}
	}

	fields := make([]domain.SchemaField, 0, len(seen))
	for path, fieldType := range seen {
		fields = append(fields, domain.SchemaField{Path: path, Type: fieldType})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].Path < fields[j].Path })

	cut := false
	if len(fields) > maxSchemaFieldsPerIndex {
		fields, cut = fields[:maxSchemaFieldsPerIndex], true
	}
	return fields, cut, nil
}

// walkProperties records every field under a "properties" object, including
// object and nested sub-fields and multi-fields such as "title.keyword".
func walkProperties(raw json.RawMessage, prefix string, depth int, out map[string]string) error {
	if depth > maxMappingDepth {
		return nil
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal(raw, &properties); err != nil {
		return err
	}

	for name, rawField := range properties {
		var field struct {
			Type       string                     `json:"type"`
			Properties json.RawMessage            `json:"properties"`
			Fields     map[string]json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(rawField, &field); err != nil {
			return err
		}

		path := prefix + name
		fieldType := field.Type
		if fieldType == "" {
			fieldType = defaultTypeForUntypedLeaf // an object mapping omits its type
		}
		out[path] = fieldType

		if len(field.Properties) > 0 {
			if err := walkProperties(field.Properties, path+".", depth+1, out); err != nil {
				return err
			}
		}
		for subName, rawSub := range field.Fields {
			var sub struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(rawSub, &sub); err != nil {
				return err
			}
			out[path+"."+subName] = sub.Type
		}
	}
	return nil
}
