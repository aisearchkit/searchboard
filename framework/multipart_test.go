package framework_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/network"
	"github.com/searchboard/searchboard/framework/ut"
)

func multipartBody(t *testing.T, fields map[string]string, files map[string]string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range files {
		part, err := w.CreateFormFile(name, name+".txt")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(part, content)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), w.FormDataContentType()
}

type upload struct {
	Name string `form:"name" binding:"required"`
	Note string `form:"note"`
}

func multipartServer() *framework.Server {
	s := newServer()
	s.POST("/upload", func(_ context.Context, c *framework.Context) {
		header, err := c.FormFile("avatar")
		if err != nil {
			c.String(400, "%v", err)
			return
		}
		file, err := header.Open()
		if err != nil {
			c.String(500, "%v", err)
			return
		}
		defer file.Close()
		content, _ := io.ReadAll(file)
		c.String(200, "%s|%s|%d|%s|%s", c.PostForm("name"), header.Filename, header.Size, content, c.PostForm("missing"))
	})
	s.POST("/bind", func(_ context.Context, c *framework.Context) {
		var in upload
		if err := c.Bind(&in); err != nil {
			c.String(400, "%v", err)
			return
		}
		c.String(200, "%s/%s", in.Name, in.Note)
	})
	s.POST("/form", func(_ context.Context, c *framework.Context) {
		c.String(200, "%s", c.PostForm("name"))
	})
	return s
}

func TestMultipartUpload(t *testing.T) {
	s := multipartServer()
	body, contentType := multipartBody(t, map[string]string{"name": "ada"}, map[string]string{"avatar": "file-bytes"})

	got := ut.PerformRequest(s, "POST", "/upload", body, ut.Header{Key: "Content-Type", Value: contentType})
	if want := "ada|avatar.txt|10|file-bytes|"; got.Status != 200 || got.BodyString() != want {
		t.Fatalf("got %d %q, want %q", got.Status, got.BodyString(), want)
	}
}

func TestMultipartErrors(t *testing.T) {
	s := multipartServer()
	body, contentType := multipartBody(t, map[string]string{"name": "ada"}, nil)

	if got := ut.PerformRequest(s, "POST", "/upload", body, ut.Header{Key: "Content-Type", Value: contentType}); got.Status != 400 || got.BodyString() != http.ErrMissingFile.Error() {
		t.Errorf("missing file = %d %q", got.Status, got.BodyString())
	}
	if got := ut.PerformRequest(s, "POST", "/upload", []byte("x=1"), ut.Header{Key: "Content-Type", Value: "application/x-www-form-urlencoded"}); got.Status != 400 || got.BodyString() != framework.ErrNotMultipart.Error() {
		t.Errorf("non-multipart body = %d %q", got.Status, got.BodyString())
	}
	if got := ut.PerformRequest(s, "POST", "/upload", []byte("garbage"), ut.Header{Key: "Content-Type", Value: "multipart/form-data; boundary=zzz"}); got.Status != 400 {
		t.Errorf("malformed multipart = %d", got.Status)
	}
	if got := ut.PerformRequest(s, "POST", "/upload", body, ut.Header{Key: "Content-Type", Value: "multipart/form-data"}); got.Status != 400 {
		t.Errorf("missing boundary = %d", got.Status)
	}
}

func TestBindMultipartAndUrlEncodedForms(t *testing.T) {
	s := multipartServer()
	body, contentType := multipartBody(t, map[string]string{"name": "ada", "note": "hi"}, nil)

	if got := ut.PerformRequest(s, "POST", "/bind", body, ut.Header{Key: "Content-Type", Value: contentType}); got.BodyString() != "ada/hi" {
		t.Errorf("multipart bind = %d %q", got.Status, got.BodyString())
	}
	if got := ut.PerformRequest(s, "POST", "/bind", []byte("name=grace&note=yo"), ut.Header{Key: "Content-Type", Value: "application/x-www-form-urlencoded"}); got.BodyString() != "grace/yo" {
		t.Errorf("urlencoded bind = %d %q", got.Status, got.BodyString())
	}
	body, contentType = multipartBody(t, map[string]string{"note": "no name"}, nil)
	if got := ut.PerformRequest(s, "POST", "/bind", body, ut.Header{Key: "Content-Type", Value: contentType}); got.Status != 400 {
		t.Errorf("required field missing = %d", got.Status)
	}
	if got := ut.PerformRequest(s, "POST", "/form", []byte("name=z"), ut.Header{Key: "Content-Type", Value: "application/x-www-form-urlencoded; charset=utf-8"}); got.BodyString() != "z" {
		t.Errorf("PostForm with charset parameter = %q", got.BodyString())
	}
}

func TestMultipartOverTheWire(t *testing.T) {
	forEachTransport(t, func(t *testing.T, newTransport func() network.Transport) {
		r := start(t, newTransport(), func(s *framework.Server) {
			s.POST("/upload", func(_ context.Context, c *framework.Context) {
				header, err := c.FormFile("blob")
				if err != nil {
					c.String(400, "%v", err)
					return
				}
				c.String(200, "%s:%d", c.PostForm("kind"), header.Size)
			})
		})

		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		_ = w.WriteField("kind", "logs")
		part, _ := w.CreateFormFile("blob", "data.bin")
		_, _ = part.Write(bytes.Repeat([]byte{7}, 1<<20))
		_ = w.Close()

		resp, err := newClient().Post(r.url("/upload"), w.FormDataContentType(), &body)
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, resp); got != "logs:1048576" {
			t.Fatalf("got %q", got)
		}
	})
}
