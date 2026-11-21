package framework

import (
	"net/http"
	"path"
	"strings"
)

// RouterGroup is a set of routes sharing a path prefix and middleware.
type RouterGroup struct {
	handlers HandlersChain
	basePath string
	server   *Server
}

// Use appends middleware that applies to routes registered on this group
// afterwards. Routes registered before the call are not affected.
func (g *RouterGroup) Use(middleware ...HandlerFunc) *RouterGroup {
	g.handlers = append(g.handlers, middleware...)
	return g
}

// Group creates a sub-group. The new group inherits this group's middleware
// and prefix, and any extra middleware given here.
func (g *RouterGroup) Group(relativePath string, middleware ...HandlerFunc) *RouterGroup {
	return &RouterGroup{
		handlers: combine(g.handlers, middleware),
		basePath: joinPaths(g.basePath, relativePath),
		server:   g.server,
	}
}

// BasePath returns the prefix of the group.
func (g *RouterGroup) BasePath() string { return g.basePath }

// Handle registers handlers for a method and path. It panics if no handler is
// given or if the route conflicts with an existing one.
func (g *RouterGroup) Handle(method, relativePath string, handlers ...HandlerFunc) {
	if len(handlers) == 0 {
		panic("framework: route " + method + " " + relativePath + " has no handler")
	}
	g.server.trees.Add(method, joinPaths(g.basePath, relativePath), combine(g.handlers, handlers))
}

// GET registers a handler for GET requests.
func (g *RouterGroup) GET(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodGet, path, handlers...)
}

// POST registers a handler for POST requests.
func (g *RouterGroup) POST(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPost, path, handlers...)
}

// PUT registers a handler for PUT requests.
func (g *RouterGroup) PUT(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPut, path, handlers...)
}

// PATCH registers a handler for PATCH requests.
func (g *RouterGroup) PATCH(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodPatch, path, handlers...)
}

// DELETE registers a handler for DELETE requests.
func (g *RouterGroup) DELETE(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodDelete, path, handlers...)
}

// HEAD registers a handler for HEAD requests.
func (g *RouterGroup) HEAD(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodHead, path, handlers...)
}

// OPTIONS registers a handler for OPTIONS requests.
func (g *RouterGroup) OPTIONS(path string, handlers ...HandlerFunc) {
	g.Handle(http.MethodOptions, path, handlers...)
}

// Any registers handlers for GET, POST, PUT, PATCH, DELETE, HEAD and OPTIONS.
func (g *RouterGroup) Any(path string, handlers ...HandlerFunc) {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodHead, http.MethodOptions,
	} {
		g.Handle(method, path, handlers...)
	}
}

func combine(a, b HandlersChain) HandlersChain {
	merged := make(HandlersChain, 0, len(a)+len(b))
	merged = append(merged, a...)
	return append(merged, b...)
}

// joinPaths joins a prefix and a relative path, keeping a trailing slash that
// the relative path carries ("/api" + "/" is "/api/").
func joinPaths(base, relative string) string {
	if relative == "" {
		return base
	}
	joined := path.Join(base, relative)
	if strings.HasSuffix(relative, "/") && !strings.HasSuffix(joined, "/") {
		return joined + "/"
	}
	return joined
}
