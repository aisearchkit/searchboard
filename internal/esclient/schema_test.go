package esclient

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/searchboard/searchboard/internal/domain"
)

func schemaPaths(index domain.SchemaIndex) map[string]string {
	paths := map[string]string{}
	for _, f := range index.Fields {
		paths[f.Path] = f.Type
	}
	return paths
}

const typelessMapping = `{
  "logs-2026": {"mappings": {"properties": {
    "@timestamp": {"type": "date"},
    "message": {"type": "text", "fields": {"keyword": {"type": "keyword", "ignore_above": 256}}},
    "user": {"properties": {"name": {"type": "keyword"}, "address": {"properties": {"city": {"type": "keyword"}}}}},
    "tags": {"type": "nested", "properties": {"key": {"type": "keyword"}}}
  }}},
  ".kibana": {"mappings": {"properties": {"type": {"type": "keyword"}}}},
  "empty": {"mappings": {}}
}`

const aliasesBody = `{
  "logs-2026": {"aliases": {"logs": {}, "all-logs": {}}},
  ".kibana": {"aliases": {}},
  "empty": {"aliases": {}}
}`

func TestSchemaTypelessMappings(t *testing.T) {
	fake := newFakeCluster("", "8.15.0", map[string]string{"/_mapping": typelessMapping, "/_alias": aliasesBody})
	schema, err := fake.start(t, Config{}).Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if names := []string{schema.Indices[0].Name, schema.Indices[1].Name, schema.Indices[2].Name}; !reflect.DeepEqual(names, []string{"empty", "logs-2026", ".kibana"}) {
		t.Fatalf("index order = %v, want regular indices first and hidden ones last", names)
	}

	logs := schema.Indices[1]
	if !reflect.DeepEqual(logs.Aliases, []string{"all-logs", "logs"}) {
		t.Errorf("aliases = %v", logs.Aliases)
	}
	want := map[string]string{
		"@timestamp": "date", "message": "text", "message.keyword": "keyword",
		"user": "object", "user.name": "keyword", "user.address": "object", "user.address.city": "keyword",
		"tags": "nested", "tags.key": "keyword",
	}
	if got := schemaPaths(logs); !reflect.DeepEqual(got, want) {
		t.Errorf("fields = %v\nwant     %v", got, want)
	}
	if len(schema.Indices[0].Fields) != 0 || schema.Truncated {
		t.Errorf("an index without properties must have no fields; truncated=%v", schema.Truncated)
	}

	if q := fake.query("/_mapping"); q.Get("filter_path") != "*.mappings" || q.Get("expand_wildcards") != "open" {
		t.Errorf("mapping query = %v", q)
	}
}

func TestSchemaTypedMappingsFromElasticsearch5And6(t *testing.T) {
	const typed = `{
	  "orders": {"mappings": {"_doc": {"properties": {
	    "id": {"type": "keyword"},
	    "customer": {"properties": {"name": {"type": "text"}}}
	  }}}},
	  "legacy": {"mappings": {
	    "tweet": {"properties": {"text": {"type": "text"}}},
	    "user":  {"properties": {"login": {"type": "keyword"}}}
	  }}
	}`
	fake := newFakeCluster("", "6.8.0", map[string]string{"/_mapping": typed, "/_alias": `{"orders":{"aliases":{}},"legacy":{"aliases":{}}}`})
	schema, err := fake.start(t, Config{}).Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]domain.SchemaIndex{}
	for _, index := range schema.Indices {
		byName[index.Name] = index
	}
	if got := schemaPaths(byName["orders"]); !reflect.DeepEqual(got, map[string]string{"id": "keyword", "customer": "object", "customer.name": "text"}) {
		t.Errorf("orders fields = %v", got)
	}
	if got := schemaPaths(byName["legacy"]); !reflect.DeepEqual(got, map[string]string{"text": "text", "login": "keyword"}) {
		t.Errorf("fields of every type must be merged: %v", got)
	}
}

func TestSchemaIgnoresNonMappingSettings(t *testing.T) {
	const mapping = `{"idx": {"mappings": {"dynamic": "strict", "_meta": {"owner": "me"}, "properties": {"a": {"type": "keyword"}}}}}`
	fake := newFakeCluster("opensearch", "2.11.0", map[string]string{"/_mapping": mapping, "/_alias": `{"idx":{"aliases":{}}}`})
	schema, err := fake.start(t, Config{}).Schema(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := schemaPaths(schema.Indices[0]); !reflect.DeepEqual(got, map[string]string{"a": "keyword"}) {
		t.Fatalf("fields = %v", got)
	}
}

func TestSchemaLimits(t *testing.T) {
	t.Run("too many indices", func(t *testing.T) {
		var mapping, alias strings.Builder
		mapping.WriteString("{")
		alias.WriteString("{")
		for i := 0; i < maxSchemaIndices+5; i++ {
			if i > 0 {
				mapping.WriteString(",")
				alias.WriteString(",")
			}
			fmt.Fprintf(&mapping, `"idx-%05d":{"mappings":{"properties":{"f":{"type":"keyword"}}}}`, i)
			fmt.Fprintf(&alias, `"idx-%05d":{"aliases":{}}`, i)
		}
		mapping.WriteString("}")
		alias.WriteString("}")

		fake := newFakeCluster("", "8.0.0", map[string]string{"/_mapping": mapping.String(), "/_alias": alias.String()})
		schema, err := fake.start(t, Config{}).Schema(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(schema.Indices) != maxSchemaIndices || !schema.Truncated {
			t.Fatalf("indices = %d truncated = %v", len(schema.Indices), schema.Truncated)
		}
	})

	t.Run("too many fields in one index", func(t *testing.T) {
		var props strings.Builder
		for i := 0; i < maxSchemaFieldsPerIndex+10; i++ {
			if i > 0 {
				props.WriteString(",")
			}
			fmt.Fprintf(&props, `"f%05d":{"type":"keyword"}`, i)
		}
		mapping := `{"wide":{"mappings":{"properties":{` + props.String() + `}}}}`
		fake := newFakeCluster("", "8.0.0", map[string]string{"/_mapping": mapping, "/_alias": `{"wide":{"aliases":{}}}`})
		schema, err := fake.start(t, Config{}).Schema(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(schema.Indices[0].Fields) != maxSchemaFieldsPerIndex || !schema.Truncated {
			t.Fatalf("fields = %d truncated = %v", len(schema.Indices[0].Fields), schema.Truncated)
		}
	})

	t.Run("pathologically deep mapping", func(t *testing.T) {
		deep := `{"type":"keyword"}`
		for i := 0; i < maxMappingDepth+10; i++ {
			deep = `{"properties":{"n":` + deep + `}}`
		}
		fake := newFakeCluster("", "8.0.0", map[string]string{"/_mapping": `{"deep":{"mappings":{"properties":{"root":` + deep + `}}}}`, "/_alias": `{"deep":{"aliases":{}}}`})
		if _, err := fake.start(t, Config{}).Schema(context.Background()); err != nil {
			t.Fatalf("a deep mapping must be cut off, not fail: %v", err)
		}
	})
}

func TestSchemaPropagatesClusterErrors(t *testing.T) {
	fake := newFakeCluster("", "8.0.0", map[string]string{"/_mapping": `!403 {"error":"forbidden"}`, "/_alias": `{}`})
	if _, err := fake.start(t, Config{}).Schema(context.Background()); !HasStatus(err, 403) {
		t.Fatalf("error = %v, want an HTTP 403 APIError", err)
	}
}
