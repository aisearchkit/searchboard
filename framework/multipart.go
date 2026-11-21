package framework

import (
	"bytes"
	"errors"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
)

// ErrNotMultipart is returned when a multipart accessor is used on a request
// whose Content-Type is not multipart/form-data with a boundary.
var ErrNotMultipart = errors.New("framework: request is not multipart/form-data")

// MultipartForm parses the request body as multipart/form-data once and caches
// the result for the lifetime of the request. The whole body is already in
// memory (it is bounded by WithMaxBodyBytes), so file parts are kept in memory
// and no temporary files are created.
func (c *Context) MultipartForm() (*multipart.Form, error) {
	if c.multipartDone {
		return c.multipart, c.multipartErr
	}
	c.multipartDone = true

	media, params, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" || params["boundary"] == "" {
		c.multipartErr = ErrNotMultipart
		return nil, c.multipartErr
	}

	reader := multipart.NewReader(bytes.NewReader(c.Request.Body()), params["boundary"])
	form, err := reader.ReadForm(c.server.opts.limits.MaxBodyBytes)
	if err != nil {
		c.multipartErr = err
		return nil, err
	}
	c.multipart = form
	return form, nil
}

// FormFile returns the first uploaded file for the form field name.
func (c *Context) FormFile(name string) (*multipart.FileHeader, error) {
	form, err := c.MultipartForm()
	if err != nil {
		return nil, err
	}
	if files := form.File[name]; len(files) > 0 {
		return files[0], nil
	}
	return nil, http.ErrMissingFile
}

// PostForm returns the first body form value for key from an
// application/x-www-form-urlencoded or multipart/form-data body, or "".
func (c *Context) PostForm(key string) string {
	return c.postForm().Get(key)
}

// postForm returns the parsed body form values, parsing the body on first use.
func (c *Context) postForm() url.Values {
	if c.formDone {
		return c.form
	}
	c.formDone = true

	switch mediaType(c.Request.Header.Get("Content-Type")) {
	case "application/x-www-form-urlencoded":
		c.form, _ = url.ParseQuery(string(c.Request.Body()))
	case "multipart/form-data":
		if form, err := c.MultipartForm(); err == nil {
			c.form = url.Values(form.Value)
		}
	}
	if c.form == nil {
		c.form = url.Values{}
	}
	return c.form
}

// releaseMultipart removes any resources held by a parsed multipart form.
func (c *Context) releaseMultipart() {
	if c.multipart != nil {
		_ = c.multipart.RemoveAll()
	}
	c.multipart, c.multipartErr, c.multipartDone = nil, nil, false
}
