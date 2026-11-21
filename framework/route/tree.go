// Package route implements the routing table: a segment trie with static,
// ":param" and "*catchall" segments.
//
// Matching priority at every segment is static > param > catch-all, with
// backtracking, so "/users/me" wins over "/users/:id" for that exact path while
// "/users/42" still reaches the param route.
package route

import (
	"fmt"
	"sort"
	"strings"
)

// Param is one captured path parameter.
type Param struct {
	Key   string
	Value string
}

// Params is the ordered list of parameters captured while matching.
type Params []Param

// Get returns the value of the named parameter.
func (ps Params) Get(name string) (string, bool) {
	for i := range ps {
		if ps[i].Key == name {
			return ps[i].Value, true
		}
	}
	return "", false
}

// Info describes a registered route.
type Info struct {
	Method string
	Path   string
}

// Tree maps (method, path) to a value of type H, typically a handler chain.
// Registration is not safe for concurrent use; lookups are, once registration
// has finished.
type Tree[H any] struct {
	roots map[string]*node[H]
}

// NewTree returns an empty tree.
func NewTree[H any]() *Tree[H] {
	return &Tree[H]{roots: map[string]*node[H]{}}
}

type node[H any] struct {
	static   map[string]*node[H]
	param    *node[H]
	paramKey string
	wild     *node[H]
	wildKey  string

	value    H
	has      bool
	fullPath string
}

// Add registers value for method and path. It panics on programmer errors:
// a path that does not start with "/", an empty or misplaced parameter name,
// a conflicting parameter name at the same position, or a duplicate route.
func (t *Tree[H]) Add(method, path string, value H) {
	if method == "" {
		panic("route: empty method")
	}
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("route: path %q must begin with '/'", path))
	}

	root := t.roots[method]
	if root == nil {
		root = &node[H]{}
		t.roots[method] = root
	}

	segments := strings.Split(path[1:], "/")
	n := root
	for i, segment := range segments {
		switch {
		case strings.HasPrefix(segment, ":"):
			name := parameterName(path, segment)
			if n.param == nil {
				n.param = &node[H]{}
				n.paramKey = name
			} else if n.paramKey != name {
				panic(fmt.Sprintf("route: %q uses parameter :%s where an existing route uses :%s", path, name, n.paramKey))
			}
			n = n.param
		case strings.HasPrefix(segment, "*"):
			name := parameterName(path, segment)
			if i != len(segments)-1 {
				panic(fmt.Sprintf("route: catch-all *%s must be the last segment of %q", name, path))
			}
			if n.wild != nil {
				panic(fmt.Sprintf("route: %q conflicts with an existing catch-all", path))
			}
			n.wild = &node[H]{}
			n.wildKey = name
			n = n.wild
		default:
			if n.static == nil {
				n.static = map[string]*node[H]{}
			}
			child := n.static[segment]
			if child == nil {
				child = &node[H]{}
				n.static[segment] = child
			}
			n = child
		}
	}

	if n.has {
		panic(fmt.Sprintf("route: %s %q is already registered", method, path))
	}
	n.value, n.has, n.fullPath = value, true, path
}

func parameterName(path, segment string) string {
	name := segment[1:]
	if name == "" || strings.ContainsAny(name, ":*") {
		panic(fmt.Sprintf("route: invalid parameter %q in %q", segment, path))
	}
	return name
}

// Find looks up method and path. Captured parameters are appended to *params.
// The returned fullPath is the registered pattern, for example "/users/:id".
func (t *Tree[H]) Find(method, path string, params *Params) (value H, fullPath string, ok bool) {
	root := t.roots[method]
	if root == nil || !strings.HasPrefix(path, "/") {
		return value, "", false
	}
	if found := root.walk(path, 1, params); found != nil {
		return found.value, found.fullPath, true
	}
	return value, "", false
}

func (n *node[H]) walk(path string, start int, params *Params) *node[H] {
	end := strings.IndexByte(path[start:], '/')
	last := end < 0
	if last {
		end = len(path)
	} else {
		end += start
	}
	segment := path[start:end]

	if child := n.static[segment]; child != nil {
		if last {
			if child.has {
				return child
			}
		} else if found := child.walk(path, end+1, params); found != nil {
			return found
		}
	}

	// A parameter never matches an empty segment, so "/users/" does not hit "/users/:id".
	if n.param != nil && segment != "" {
		mark := len(*params)
		*params = append(*params, Param{Key: n.paramKey, Value: segment})
		if last {
			if n.param.has {
				return n.param
			}
		} else if found := n.param.walk(path, end+1, params); found != nil {
			return found
		}
		*params = (*params)[:mark]
	}

	if n.wild != nil {
		// The value keeps its leading slash: "/files/a/b" captures "/a/b".
		*params = append(*params, Param{Key: n.wildKey, Value: path[start-1:]})
		return n.wild
	}
	return nil
}

// Allowed returns the sorted methods that have a route matching path. It is
// used to answer 405 Method Not Allowed.
func (t *Tree[H]) Allowed(path string) []string {
	var methods []string
	var scratch Params
	for method := range t.roots {
		scratch = scratch[:0]
		if _, _, ok := t.Find(method, path, &scratch); ok {
			methods = append(methods, method)
		}
	}
	sort.Strings(methods)
	return methods
}

// Routes lists every registered route sorted by path then method.
func (t *Tree[H]) Routes() []Info {
	var routes []Info
	for method, root := range t.roots {
		root.collect(method, &routes)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path != routes[j].Path {
			return routes[i].Path < routes[j].Path
		}
		return routes[i].Method < routes[j].Method
	})
	return routes
}

func (n *node[H]) collect(method string, out *[]Info) {
	if n.has {
		*out = append(*out, Info{Method: method, Path: n.fullPath})
	}
	for _, child := range n.static {
		child.collect(method, out)
	}
	if n.param != nil {
		n.param.collect(method, out)
	}
	if n.wild != nil {
		n.wild.collect(method, out)
	}
}
