package framework

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// maxStaticFileSize bounds how much of a file FileFS reads into memory.
const maxStaticFileSize = 64 << 20

// StaticOptions customize static file serving.
type StaticOptions struct {
	// Index is served for a directory request. Defaults to "index.html".
	Index string
	// CacheControl returns the Cache-Control value for a file name, or "" for none.
	CacheControl func(name string) string
}

// FileFS serves name from fsys. The name is cleaned first, so it can never
// escape the file system root. It returns fs.ErrNotExist when the file is
// missing or is a directory. It sets Content-Type, ETag and, when the file
// system reports a modification time, Last-Modified, and answers 304 when the
// client's If-None-Match matches.
func (c *Context) FileFS(fsys fs.FS, name string) error {
	return c.serveFile(fsys, name, StaticOptions{})
}

func (c *Context) serveFile(fsys fs.FS, name string, opts StaticOptions) error {
	name = strings.TrimPrefix(path.Clean("/"+name), "/")
	if name == "" {
		name = "."
	}
	if !fs.ValidPath(name) {
		return fs.ErrNotExist
	}

	info, err := fs.Stat(fsys, name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		index := opts.Index
		if index == "" {
			index = "index.html"
		}
		name = path.Join(name, index)
		if info, err = fs.Stat(fsys, name); err != nil {
			return err
		}
		if info.IsDir() {
			return fs.ErrNotExist
		}
	}

	file, err := fsys.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxStaticFileSize+1))
	if err != nil {
		return err
	}
	if len(content) > maxStaticFileSize {
		return fmt.Errorf("framework: %s is larger than %d bytes", name, maxStaticFileSize)
	}

	etag := staticETag(content, info)
	c.SetHeader("ETag", etag)
	if modified := info.ModTime(); !modified.IsZero() {
		c.SetHeader("Last-Modified", modified.UTC().Format(http.TimeFormat))
	}
	if opts.CacheControl != nil {
		if value := opts.CacheControl(name); value != "" {
			c.SetHeader("Cache-Control", value)
		}
	}

	if etagMatches(c.GetHeader("If-None-Match"), etag) {
		c.NoContent(http.StatusNotModified)
		return nil
	}
	c.Data(http.StatusOK, contentTypeFor(name, content), content)
	return nil
}

func contentTypeFor(name string, content []byte) string {
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		return contentType
	}
	return http.DetectContentType(content)
}

// staticETag is size and modification time when the file system has one,
// otherwise a short content hash (embedded files carry no modification time).
func staticETag(content []byte, info fs.FileInfo) string {
	if modified := info.ModTime(); !modified.IsZero() {
		return fmt.Sprintf(`"%x-%x"`, info.Size(), modified.UnixNano())
	}
	sum := sha256.Sum256(content)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}

// FileHandler returns a handler that serves the request path from fsys. Use it
// as a NoRoute handler to serve a single-page application after the API routes
// have had their chance. Missing files answer 404.
func FileHandler(fsys fs.FS, opts StaticOptions) HandlerFunc {
	return func(_ context.Context, c *Context) {
		name, err := url.PathUnescape(c.Request.Path)
		if err != nil {
			c.String(http.StatusBadRequest, "bad request path")
			return
		}
		respondFile(c, fsys, name, opts)
	}
}

// StaticFS serves fsys under relativePath for GET and HEAD, for example
// StaticFS("/assets", assetsFS) serves /assets/app.js from the file "app.js".
func (g *RouterGroup) StaticFS(relativePath string, fsys fs.FS, opts ...StaticOptions) {
	var options StaticOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	handler := func(_ context.Context, c *Context) {
		respondFile(c, fsys, c.Param("filepath"), options)
	}
	pattern := joinPaths(relativePath, "/*filepath")
	g.GET(pattern, handler)
	g.HEAD(pattern, handler)
}

func respondFile(c *Context, fsys fs.FS, name string, opts StaticOptions) {
	switch err := c.serveFile(fsys, name, opts); {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		c.String(http.StatusNotFound, "404 page not found")
	default:
		c.server.opts.logger.Error("serve static file", "err", err, "name", name)
		c.String(http.StatusInternalServerError, "internal server error")
	}
}
