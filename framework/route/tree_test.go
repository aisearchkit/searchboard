package route

import (
	"reflect"
	"testing"
)

func newTestTree() *Tree[string] {
	tree := NewTree[string]()
	for _, path := range []string{
		"/",
		"/users",
		"/users/",
		"/users/me",
		"/users/:id",
		"/users/:id/posts",
		"/users/:id/posts/:post",
		"/files/*path",
		"/a/b/c",
		"/a/:x/d",
	} {
		tree.Add("GET", path, path)
	}
	tree.Add("POST", "/users", "POST /users")
	return tree
}

func TestFind(t *testing.T) {
	tests := []struct {
		path       string
		want       string
		wantParams Params
	}{
		{"/", "/", nil},
		{"/users", "/users", nil},
		{"/users/", "/users/", nil},
		{"/users/me", "/users/me", nil}, // static beats param
		{"/users/42", "/users/:id", Params{{"id", "42"}}},
		{"/users/42/posts", "/users/:id/posts", Params{{"id", "42"}}},
		{"/users/42/posts/7", "/users/:id/posts/:post", Params{{"id", "42"}, {"post", "7"}}},
		{"/users/me/posts", "/users/:id/posts", Params{{"id", "me"}}}, // backtracks from static "me"
		{"/files/a/b.txt", "/files/*path", Params{{"path", "/a/b.txt"}}},
		{"/files/", "/files/*path", Params{{"path", "/"}}},
		{"/a/b/c", "/a/b/c", nil},
		{"/a/b/d", "/a/:x/d", Params{{"x", "b"}}}, // backtracks out of the static "b" branch
		{"/users/a%20b", "/users/:id", Params{{"id", "a%20b"}}},
	}
	tree := newTestTree()
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			var params Params
			got, fullPath, ok := tree.Find("GET", tt.path, &params)
			if !ok || got != tt.want || fullPath != tt.want {
				t.Fatalf("Find = %q, %q, %v; want %q", got, fullPath, ok, tt.want)
			}
			if !reflect.DeepEqual(params, tt.wantParams) && !(len(params) == 0 && len(tt.wantParams) == 0) {
				t.Fatalf("params = %v, want %v", params, tt.wantParams)
			}
		})
	}
}

func TestFindMisses(t *testing.T) {
	tree := newTestTree()
	for _, path := range []string{"/nope", "/users/42/unknown", "/files", "/a/b", "/a/b/c/d", "users", ""} {
		var params Params
		if _, _, ok := tree.Find("GET", path, &params); ok {
			t.Errorf("GET %q unexpectedly matched", path)
		}
	}
	var params Params
	if _, _, ok := tree.Find("DELETE", "/users", &params); ok {
		t.Error("an unregistered method must not match")
	}
}

func TestParamNeverMatchesEmptySegment(t *testing.T) {
	tree := NewTree[string]()
	tree.Add("GET", "/users/:id", "user")
	var params Params
	if _, _, ok := tree.Find("GET", "/users/", &params); ok {
		t.Fatal("empty segment must not bind a parameter")
	}
}

func TestFailedBranchDoesNotLeakParams(t *testing.T) {
	tree := NewTree[string]()
	tree.Add("GET", "/:a/x", "first")
	tree.Add("GET", "/:a/y/*rest", "second")

	var params Params
	got, _, ok := tree.Find("GET", "/1/y/z", &params)
	if !ok || got != "second" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	if !reflect.DeepEqual(params, Params{{"a", "1"}, {"rest", "/z"}}) {
		t.Fatalf("params = %v", params)
	}
}

func TestAllowed(t *testing.T) {
	tree := newTestTree()
	if got := tree.Allowed("/users"); !reflect.DeepEqual(got, []string{"GET", "POST"}) {
		t.Fatalf("Allowed(/users) = %v", got)
	}
	if got := tree.Allowed("/nope"); len(got) != 0 {
		t.Fatalf("Allowed(/nope) = %v", got)
	}
}

func TestRoutes(t *testing.T) {
	tree := NewTree[int]()
	tree.Add("POST", "/b", 1)
	tree.Add("GET", "/b", 2)
	tree.Add("GET", "/a/:id", 3)
	want := []Info{{"GET", "/a/:id"}, {"GET", "/b"}, {"POST", "/b"}}
	if got := tree.Routes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Routes = %v, want %v", got, want)
	}
}

func TestParamsGet(t *testing.T) {
	params := Params{{"a", "1"}, {"b", "2"}}
	if v, ok := params.Get("b"); !ok || v != "2" {
		t.Fatalf("Get(b) = %q, %v", v, ok)
	}
	if _, ok := params.Get("c"); ok {
		t.Fatal("Get(c) must report absence")
	}
}

func TestAddPanics(t *testing.T) {
	tests := map[string]func(*Tree[string]){
		"no leading slash":     func(tr *Tree[string]) { tr.Add("GET", "users", "x") },
		"empty method":         func(tr *Tree[string]) { tr.Add("", "/x", "x") },
		"duplicate":            func(tr *Tree[string]) { tr.Add("GET", "/a", "x"); tr.Add("GET", "/a", "y") },
		"param name conflict":  func(tr *Tree[string]) { tr.Add("GET", "/u/:id", "x"); tr.Add("GET", "/u/:name/p", "y") },
		"empty param name":     func(tr *Tree[string]) { tr.Add("GET", "/u/:", "x") },
		"catch-all not last":   func(tr *Tree[string]) { tr.Add("GET", "/u/*rest/more", "x") },
		"two catch-alls":       func(tr *Tree[string]) { tr.Add("GET", "/u/*a", "x"); tr.Add("GET", "/u/*b", "y") },
		"param with extra sym": func(tr *Tree[string]) { tr.Add("GET", "/u/:a:b", "x") },
	}
	for name, register := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected a panic")
				}
			}()
			register(NewTree[string]())
		})
	}
}

func TestSamePatternOnDifferentMethodsIsAllowed(t *testing.T) {
	tree := NewTree[string]()
	tree.Add("GET", "/u/:id", "get")
	tree.Add("PUT", "/u/:name", "put") // parameter names are per method tree
	var params Params
	if got, _, ok := tree.Find("PUT", "/u/5", &params); !ok || got != "put" || params[0].Key != "name" {
		t.Fatalf("got %q %v ok=%v", got, params, ok)
	}
}
