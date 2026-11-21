package codec

import "testing"

func TestMarshalMatchesStandardLibraryBehavior(t *testing.T) {
	got, err := Marshal(map[string]any{"b": "<x>", "a": 1})
	if err != nil {
		t.Fatal(err)
	}
	// Sorted keys and HTML escaping, exactly like encoding/json.
	if want := `{"a":1,"b":"\u003cx\u003e"}`; string(got) != want {
		t.Fatalf("Marshal = %s, want %s", got, want)
	}
}

func TestRoundTripAndErrors(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}
	var out payload
	if err := Unmarshal([]byte(`{"name":"ada","n":3}`), &out); err != nil || out != (payload{"ada", 3}) {
		t.Fatalf("Unmarshal = %+v, %v", out, err)
	}
	if err := Unmarshal([]byte(`{"name":`), &out); err == nil {
		t.Fatal("malformed JSON must fail")
	}
	if _, err := Marshal(make(chan int)); err == nil {
		t.Fatal("unencodable value must fail")
	}
}
