package framework

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/url"

	"github.com/searchboard/searchboard/framework/binding"
	"github.com/searchboard/searchboard/framework/codec"
	"github.com/searchboard/searchboard/framework/protocol"
	"github.com/searchboard/searchboard/framework/route"
)

// HandlerFunc handles a request. Middleware has the same signature and calls
// c.Next(ctx) to run the rest of the chain.
type HandlerFunc func(ctx context.Context, c *Context)

// HandlersChain is an ordered list of middleware followed by the final handler.
type HandlersChain []HandlerFunc

const (
	mimeJSON = "application/json; charset=utf-8"
	mimeText = "text/plain; charset=utf-8"
)

// Context carries one request through the handler chain. Instances are pooled:
// do not retain a Context, or anything obtained from it, after the handler returns.
type Context struct {
	Request  *protocol.Request
	Response *protocol.Response
	// Params holds the path parameters of the matched route.
	Params route.Params

	server   *Server
	handlers HandlersChain
	index    int
	aborted  bool
	fullPath string
	keys     map[string]any
	form     url.Values
	formDone bool

	multipart     *multipart.Form
	multipartErr  error
	multipartDone bool

	ownRequest  *protocol.Request
	ownResponse *protocol.Response
}

func newContext(s *Server) *Context {
	c := &Context{
		server:      s,
		ownRequest:  &protocol.Request{},
		ownResponse: &protocol.Response{StatusCode: 200},
		Params:      make(route.Params, 0, 8),
	}
	c.Request, c.Response = c.ownRequest, c.ownResponse
	c.index = -1
	return c
}

// reset prepares the context for the next request.
func (c *Context) reset() {
	c.Request, c.Response = c.ownRequest, c.ownResponse
	c.Request.Reset()
	c.Response.Reset()
	c.Params = c.Params[:0]
	c.handlers, c.index, c.aborted, c.fullPath = nil, -1, false, ""
	clear(c.keys)
	c.form, c.formDone = nil, false
	c.releaseMultipart()
}

// Next runs the remaining handlers of the chain. Middleware calls it to wrap
// the code that follows; it returns when those handlers are done or the chain
// was aborted.
func (c *Context) Next(ctx context.Context) {
	c.index++
	for !c.aborted && c.index < len(c.handlers) {
		c.handlers[c.index](ctx, c)
		c.index++
	}
}

// Abort stops the chain: handlers after the current one will not run.
func (c *Context) Abort() { c.aborted = true }

// IsAborted reports whether Abort was called.
func (c *Context) IsAborted() bool { return c.aborted }

// Method returns the HTTP method.
func (c *Context) Method() string { return c.Request.Method }

// Path returns the raw request path.
func (c *Context) Path() string { return c.Request.Path }

// FullPath returns the pattern of the matched route, for example "/users/:id".
// It is empty when no route matched.
func (c *Context) FullPath() string { return c.fullPath }

// RemoteAddr returns the peer address as "ip:port".
func (c *Context) RemoteAddr() string { return c.Request.RemoteAddr }

// Param returns the percent-decoded value of a path parameter, or "" when absent.
func (c *Context) Param(name string) string {
	raw, ok := c.Params.Get(name)
	if !ok {
		return ""
	}
	if decoded, err := url.PathUnescape(raw); err == nil {
		return decoded
	}
	return raw
}

// Query returns the first query-string value for key, or "".
func (c *Context) Query(key string) string { return c.Request.Query().Get(key) }

// DefaultQuery returns the first query-string value for key, or def when absent.
func (c *Context) DefaultQuery(key, def string) string {
	if values, ok := c.Request.Query()[key]; ok && len(values) > 0 {
		return values[0]
	}
	return def
}

// QueryArray returns all query-string values for key.
func (c *Context) QueryArray(key string) []string { return c.Request.Query()[key] }

// GetHeader returns a request header value.
func (c *Context) GetHeader(key string) string { return c.Request.Header.Get(key) }

// SetHeader sets a response header, replacing existing values.
func (c *Context) SetHeader(key, value string) { c.Response.Header.Set(key, value) }

// Body returns the request body.
func (c *Context) Body() []byte { return c.Request.Body() }

// Set stores a value for the lifetime of the request.
func (c *Context) Set(key string, value any) {
	if c.keys == nil {
		c.keys = map[string]any{}
	}
	c.keys[key] = value
}

// Get returns a value stored with Set.
func (c *Context) Get(key string) (any, bool) {
	value, ok := c.keys[key]
	return value, ok
}

// GetString returns a string stored with Set, or "".
func (c *Context) GetString(key string) string {
	value, _ := c.keys[key].(string)
	return value
}

// Status sets the response status code.
func (c *Context) Status(code int) { c.Response.StatusCode = code }

// Data writes a raw body with the given content type.
func (c *Context) Data(code int, contentType string, body []byte) {
	c.Response.StatusCode = code
	c.Response.Header.Set("Content-Type", contentType)
	c.Response.SetBody(body)
}

// String writes a plain-text body built with fmt.Sprintf.
func (c *Context) String(code int, format string, args ...any) {
	text := format
	if len(args) > 0 {
		text = fmt.Sprintf(format, args...)
	}
	c.Response.StatusCode = code
	c.Response.Header.Set("Content-Type", mimeText)
	c.Response.SetBodyString(text)
}

// JSON writes obj encoded as JSON. An unencodable value yields a 500.
func (c *Context) JSON(code int, obj any) {
	body, err := codec.Marshal(obj)
	if err != nil {
		c.server.opts.logger.Error("encode JSON response", "err", err, "path", c.Request.Path)
		c.String(500, "internal server error")
		return
	}
	c.Data(code, mimeJSON, body)
}

// NoContent writes an empty response with the given status.
func (c *Context) NoContent(code int) {
	c.Response.StatusCode = code
	c.Response.SetBody(nil)
}

// Redirect answers with a redirect to location.
func (c *Context) Redirect(code int, location string) {
	c.Response.StatusCode = code
	c.Response.Header.Set("Location", location)
	c.Response.SetBody(nil)
}

// AbortWithStatus writes the status and stops the chain.
func (c *Context) AbortWithStatus(code int) {
	c.NoContent(code)
	c.Abort()
}

// AbortWithJSON writes a JSON body with the status and stops the chain.
func (c *Context) AbortWithJSON(code int, obj any) {
	c.JSON(code, obj)
	c.Abort()
}

// BindJSON decodes the JSON request body into obj and validates it.
func (c *Context) BindJSON(obj any) error { return binding.JSON(c.Request.Body(), obj) }

// BindQuery fills obj from the query string using `query` tags and validates it.
func (c *Context) BindQuery(obj any) error {
	return c.bindThenValidate(obj, "query", c.queryGetter())
}

// BindPath fills obj from the path parameters using `path` tags and validates it.
func (c *Context) BindPath(obj any) error {
	return c.bindThenValidate(obj, "path", c.pathGetter())
}

// BindHeader fills obj from request headers using `header` tags and validates it.
func (c *Context) BindHeader(obj any) error {
	return c.bindThenValidate(obj, "header", c.headerGetter())
}

// BindForm fills obj from an application/x-www-form-urlencoded or
// multipart/form-data body using `form` tags and validates it.
func (c *Context) BindForm(obj any) error {
	return c.bindThenValidate(obj, "form", c.formGetter())
}

// Bind fills obj from every source that applies: path parameters, then the
// query string, then the body chosen by Content-Type (JSON, URL-encoded or multipart form).
// Later sources override earlier ones. The result is validated once at the end.
// A non-empty body with another Content-Type is rejected.
func (c *Context) Bind(obj any) error {
	if err := binding.Bind(obj, "path", c.pathGetter()); err != nil {
		return err
	}
	if err := binding.Bind(obj, "query", c.queryGetter()); err != nil {
		return err
	}

	if len(c.Request.Body()) > 0 {
		switch mediaType(c.Request.Header.Get("Content-Type")) {
		case "application/json":
			if err := codec.Unmarshal(c.Request.Body(), obj); err != nil {
				return &binding.FieldError{Source: "json", Field: "body", Err: err}
			}
		case "application/x-www-form-urlencoded", "multipart/form-data":
			if err := binding.Bind(obj, "form", c.formGetter()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("binding: unsupported content type %q", c.Request.Header.Get("Content-Type"))
		}
	}
	return binding.Validate(obj)
}

func (c *Context) bindThenValidate(obj any, tag string, get binding.Getter) error {
	if err := binding.Bind(obj, tag, get); err != nil {
		return err
	}
	return binding.Validate(obj)
}

func (c *Context) queryGetter() binding.Getter {
	query := c.Request.Query()
	return func(key string) ([]string, bool) {
		values, ok := query[key]
		return values, ok
	}
}

func (c *Context) pathGetter() binding.Getter {
	return func(key string) ([]string, bool) {
		if _, ok := c.Params.Get(key); !ok {
			return nil, false
		}
		return []string{c.Param(key)}, true
	}
}

func (c *Context) headerGetter() binding.Getter {
	return func(key string) ([]string, bool) {
		values := c.Request.Header.Values(key)
		return values, len(values) > 0
	}
}

func (c *Context) formGetter() binding.Getter {
	form := c.postForm()
	return func(key string) ([]string, bool) {
		values, ok := form[key]
		return values, ok
	}
}
