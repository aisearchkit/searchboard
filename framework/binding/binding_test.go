package binding

import (
	"errors"
	"testing"
	"time"
)

func mapGetter(m map[string][]string) Getter {
	return func(key string) ([]string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

type paging struct {
	Page  int           `query:"page"`
	Size  uint8         `query:"size"`
	Q     string        `query:"q"`
	Debug bool          `query:"debug"`
	Ratio float64       `query:"ratio"`
	Tags  []string      `query:"tag"`
	IDs   []int         `query:"id"`
	After *int          `query:"after"`
	Wait  time.Duration `query:"wait"`
	Skip  string        `query:"-"`
	other string        //nolint:unused // unexported fields must be ignored
}

func TestBindScalarsSlicesPointersAndDurations(t *testing.T) {
	var p paging
	err := Bind(&p, "query", mapGetter(map[string][]string{
		"page": {"3"}, "size": {"50"}, "q": {"logs"}, "debug": {"true"}, "ratio": {"0.5"},
		"tag": {"a", "b"}, "id": {"1", "2", "3"}, "after": {"9"}, "wait": {"1500ms"}, "Skip": {"nope"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Page != 3 || p.Size != 50 || p.Q != "logs" || !p.Debug || p.Ratio != 0.5 {
		t.Errorf("scalars: %+v", p)
	}
	if len(p.Tags) != 2 || p.Tags[1] != "b" || len(p.IDs) != 3 || p.IDs[2] != 3 {
		t.Errorf("slices: %+v", p)
	}
	if p.After == nil || *p.After != 9 || p.Wait != 1500*time.Millisecond || p.Skip != "" {
		t.Errorf("pointer/duration/skip: %+v", p)
	}
}

func TestBindKeepsDefaultsForAbsentAndEmptyValues(t *testing.T) {
	p := paging{Page: 1, Size: 20, Q: "default"}
	err := Bind(&p, "query", mapGetter(map[string][]string{"page": {""}, "q": {""}}))
	if err != nil {
		t.Fatal(err)
	}
	if p.Page != 1 || p.Size != 20 {
		t.Errorf("empty numeric must keep the default: %+v", p)
	}
	if p.Q != "" {
		t.Errorf("an explicitly empty string is a real value: %q", p.Q)
	}
}

func TestBindReportsTheFailingField(t *testing.T) {
	tests := map[string]map[string][]string{
		"page":  {"page": {"abc"}},
		"size":  {"size": {"300"}}, // overflows uint8
		"debug": {"debug": {"maybe"}},
		"ratio": {"ratio": {"x"}},
		"wait":  {"wait": {"soon"}},
		"id":    {"id": {"1", "two"}},
	}
	for field, input := range tests {
		t.Run(field, func(t *testing.T) {
			var p paging
			err := Bind(&p, "query", mapGetter(input))
			var fe *FieldError
			if !errors.As(err, &fe) || fe.Field != field || fe.Source != "query" {
				t.Fatalf("err = %v, want a FieldError for %q", err, field)
			}
		})
	}
}

type embedded struct {
	Base string `query:"base"`
}

type withEmbedded struct {
	embedded
	Own string `query:"own"`
}

func TestBindEmbeddedStruct(t *testing.T) {
	type outer struct {
		embedded
		Own string `query:"own"`
	}
	var o outer
	if err := Bind(&o, "query", mapGetter(map[string][]string{"base": {"b"}, "own": {"o"}})); err != nil {
		t.Fatal(err)
	}
	if o.Base != "b" || o.Own != "o" {
		t.Fatalf("%+v", o)
	}
}

func TestBindRejectsBadDestinations(t *testing.T) {
	var s string
	for _, dst := range []any{nil, s, &s, paging{}, (*paging)(nil)} {
		if err := Bind(dst, "query", mapGetter(nil)); !errors.Is(err, ErrNotStructPointer) {
			t.Errorf("Bind(%T) = %v, want ErrNotStructPointer", dst, err)
		}
	}
}

func TestBindUnsupportedFieldType(t *testing.T) {
	type bad struct {
		M map[string]string `query:"m"`
	}
	var b bad
	err := Bind(&b, "query", mapGetter(map[string][]string{"m": {"x"}}))
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %v", err)
	}
}

type address struct {
	City string `json:"city" binding:"required"`
}

type signup struct {
	Name    string    `json:"name" binding:"required"`
	Age     int       `json:"age"`
	Limit   *int      `json:"limit" binding:"required"`
	Home    address   `json:"home"`
	Work    *address  `json:"work"`
	Others  []address `json:"others"`
	Comment string    `json:"comment"`
}

func TestJSONBindsAndValidates(t *testing.T) {
	var s signup
	body := []byte(`{"name":"ada","age":36,"limit":0,"home":{"city":"London"},"others":[{"city":"Paris"}]}`)
	if err := JSON(body, &s); err != nil {
		t.Fatal(err)
	}
	if s.Name != "ada" || s.Age != 36 || s.Limit == nil || *s.Limit != 0 {
		t.Fatalf("%+v", s)
	}
}

func TestValidateRequired(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{"missing name", `{"limit":1,"home":{"city":"x"}}`, "name"},
		{"missing pointer", `{"name":"a","home":{"city":"x"}}`, "limit"},
		{"missing nested", `{"name":"a","limit":1,"home":{}}`, "home.city"},
		{"missing in pointer struct", `{"name":"a","limit":1,"home":{"city":"x"},"work":{}}`, "work.city"},
		{"missing in slice", `{"name":"a","limit":1,"home":{"city":"x"},"others":[{"city":"ok"},{}]}`, "others[1].city"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s signup
			err := JSON([]byte(tt.body), &s)
			var fe *FieldError
			if !errors.As(err, &fe) || fe.Field != tt.wantField || !errors.Is(err, ErrRequired) {
				t.Fatalf("err = %v, want required error for %q", err, tt.wantField)
			}
		})
	}
}

func TestJSONRejectsMalformedBody(t *testing.T) {
	var s signup
	err := JSON([]byte(`{"name":`), &s)
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Source != "json" {
		t.Fatalf("err = %v", err)
	}
	if err := JSON([]byte(`{}`), s); !errors.Is(err, ErrNotStructPointer) {
		t.Fatalf("non-pointer destination err = %v", err)
	}
}
