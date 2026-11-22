package api

import (
	"context"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/searchboard/searchboard/framework"
)

const (
	indexFile      = "index.html"
	immutableCache = "public, max-age=31536000, immutable"
)

// spaHandler serves a single-page application from assets.
//
// Existing files are served as they are. A path without a file extension is a
// client-side route, so it gets the app shell (index.html) and survives a page
// reload. A missing path that looks like a file, such as /assets/old.js, is a
// real 404 instead of a misleading HTML page.
func spaHandler(assets fs.FS) framework.HandlerFunc {
	files := framework.FileHandler(assets, framework.StaticOptions{CacheControl: cacheControl})

	return func(ctx context.Context, c *framework.Context) {
		if c.Method() != http.MethodGet && c.Method() != http.MethodHead {
			writeError(c, http.StatusNotFound, "not found")
			return
		}

		name, err := url.PathUnescape(c.Path())
		if err != nil {
			c.String(http.StatusBadRequest, "bad request path")
			return
		}
		name = strings.TrimPrefix(path.Clean("/"+name), "/")

		if info, err := fs.Stat(assets, name); (err != nil || info.IsDir() && !hasIndex(assets, name)) && path.Ext(name) == "" {
			c.Request.Path = "/" + indexFile // client-side route: serve the app shell
		}
		files(ctx, c)
	}
}

func hasIndex(assets fs.FS, dir string) bool {
	_, err := fs.Stat(assets, path.Join(dir, indexFile))
	return err == nil
}

// cacheControl lets browsers keep Vite's content-hashed files forever and makes
// them revalidate everything else, so a new release is picked up immediately.
func cacheControl(name string) string {
	if strings.HasPrefix(name, "assets/") {
		return immutableCache
	}
	return "no-cache"
}
