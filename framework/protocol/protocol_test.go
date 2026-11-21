package protocol

import (
	"reflect"
	"testing"
)

func TestHeaderIsCaseInsensitiveAndOrdered(t *testing.T) {
	var h Header
	h.Add("content-type", "text/plain")
	h.Add("X-Tag", "a")
	h.Add("x-tag", "b")

	if got := h.Get("CONTENT-TYPE"); got != "text/plain" {
		t.Errorf("Get = %q", got)
	}
	if got := h.Values("X-TAG"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Values = %v", got)
	}
	if !h.Has("x-tag") || h.Has("missing") {
		t.Error("Has is wrong")
	}

	h.Set("X-Tag", "only")
	if got := h.Values("x-tag"); !reflect.DeepEqual(got, []string{"only"}) {
		t.Errorf("after Set: %v", got)
	}

	var order []string
	h.VisitAll(func(k, _ string) { order = append(order, k) })
	if !reflect.DeepEqual(order, []string{"Content-Type", "X-Tag"}) {
		t.Errorf("order = %v", order)
	}

	h.Del("content-type")
	if h.Has("Content-Type") || h.Len() != 1 {
		t.Errorf("Del failed: len=%d", h.Len())
	}
	h.Reset()
	if h.Len() != 0 {
		t.Error("Reset did not clear")
	}
}

func TestRequestSetTarget(t *testing.T) {
	tests := []struct {
		target, path, query, host string
		wantErr                   bool
	}{
		{target: "/a/b?x=1", path: "/a/b", query: "x=1"},
		{target: "/", path: "/"},
		{target: "/a%2Fb?q", path: "/a%2Fb", query: "q"},
		{target: "*", path: "*"},
		{target: "http://example.com/p?q=1", path: "/p", query: "q=1", host: "example.com"},
		{target: "http://example.com", path: "/", host: "example.com"},
		{target: "relative", wantErr: true},
		{target: "/with\x00nul", wantErr: true},
		{target: "/with\x7fdel", wantErr: true},
		{target: "http:///nohost", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			var r Request
			err := r.SetTarget(tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if r.Path != tt.path || r.RawQuery != tt.query || r.Host != tt.host {
				t.Fatalf("got path=%q query=%q host=%q", r.Path, r.RawQuery, r.Host)
			}
		})
	}
}

func TestRequestQueryIsCachedAndResettable(t *testing.T) {
	var r Request
	_ = r.SetTarget("/x?a=1&a=2&bad=%zz&b=3")
	q := r.Query()
	if got := q["a"]; !reflect.DeepEqual(got, []string{"1", "2"}) || q.Get("b") != "3" {
		t.Fatalf("query = %v", q)
	}

	_ = r.SetTarget("/y?c=4")
	if r.Query().Get("c") != "4" || r.Query().Get("a") != "" {
		t.Fatalf("query was not re-parsed: %v", r.Query())
	}
	r.Reset()
	if r.Path != "" || r.Query().Get("c") != "" {
		t.Fatal("Reset left state behind")
	}
}

func TestResponseReset(t *testing.T) {
	r := &Response{StatusCode: 404}
	r.Header.Set("X", "1")
	r.SetBodyString("gone")
	r.Reset()
	if r.StatusCode != 200 || r.Header.Len() != 0 || len(r.Body()) != 0 {
		t.Fatalf("Reset left state: %+v", r)
	}
	r.AppendBody([]byte("ab"))
	r.AppendBody([]byte("cd"))
	if string(r.Body()) != "abcd" {
		t.Fatalf("AppendBody = %q", r.Body())
	}
}
