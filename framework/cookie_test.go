package framework_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/searchboard/searchboard/framework"
	"github.com/searchboard/searchboard/framework/ut"
)

func TestReadCookies(t *testing.T) {
	s := newServer()
	s.GET("/c", func(_ context.Context, c *framework.Context) {
		a, okA := c.Cookie("a")
		b, okB := c.Cookie("b")
		later, okLater := c.Cookie("later")
		_, okMissing := c.Cookie("missing")
		c.String(200, "%s,%v %s,%v %s,%v %v", a, okA, b, okB, later, okLater, okMissing)
	})

	got := ut.PerformRequest(s, "GET", "/c", nil,
		ut.Header{Key: "Cookie", Value: "a=1; b=two"},
		ut.Header{Key: "Cookie", Value: "later=3"}, // HTTP/2 clients send one header per cookie
	)
	if want := "1,true two,true 3,true false"; got.BodyString() != want {
		t.Fatalf("got %q, want %q", got.BodyString(), want)
	}
}

func TestMalformedCookieHeaderIsIgnored(t *testing.T) {
	s := newServer()
	s.GET("/c", func(_ context.Context, c *framework.Context) {
		value, ok := c.Cookie("good")
		c.String(200, "%s,%v", value, ok)
	})
	got := ut.PerformRequest(s, "GET", "/c", nil,
		ut.Header{Key: "Cookie", Value: "=;;bad name=x"},
		ut.Header{Key: "Cookie", Value: "good=yes"},
	)
	if got.BodyString() != "yes,true" {
		t.Fatalf("got %q", got.BodyString())
	}
}

func TestSetCookie(t *testing.T) {
	s := newServer()
	s.GET("/set", func(_ context.Context, c *framework.Context) {
		if err := c.SetCookie(framework.SecureSessionCookie("sid", "abc", time.Hour)); err != nil {
			c.String(500, "%v", err)
			return
		}
		_ = c.SetCookie(&http.Cookie{Name: "theme", Value: "dark"})
		c.String(200, "ok")
	})

	got := ut.PerformRequest(s, "GET", "/set", nil)
	cookies := got.Header.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Fatalf("Set-Cookie headers = %v", cookies)
	}
	for _, attribute := range []string{"sid=abc", "Path=/", "Max-Age=3600", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(cookies[0], attribute) {
			t.Errorf("session cookie lacks %q: %s", attribute, cookies[0])
		}
	}
	if cookies[1] != "theme=dark" {
		t.Errorf("second cookie = %q", cookies[1])
	}
}

func TestSetCookieRejectsInvalidCookies(t *testing.T) {
	s := newServer()
	var errs []error
	s.GET("/bad", func(_ context.Context, c *framework.Context) {
		errs = append(errs,
			c.SetCookie(&http.Cookie{Name: "bad name", Value: "x"}),
			c.SetCookie(&http.Cookie{Name: "", Value: "x"}),
			c.SetCookie(&http.Cookie{Name: "ok", Value: "x", Path: "/a;b"}),
		)
		c.String(200, "done")
	})
	got := ut.PerformRequest(s, "GET", "/bad", nil)
	for i, err := range errs {
		if err == nil {
			t.Errorf("cookie %d was accepted", i)
		}
	}
	if got.Header.Has("Set-Cookie") {
		t.Errorf("invalid cookies must not produce a header: %v", got.Header.Values("Set-Cookie"))
	}
}

func TestDeleteCookie(t *testing.T) {
	s := newServer()
	s.GET("/del", func(_ context.Context, c *framework.Context) {
		_ = c.DeleteCookie("sid", "/", "")
		c.NoContent(204)
	})
	cookie := ut.PerformRequest(s, "GET", "/del", nil).Header.Get("Set-Cookie")
	if !strings.Contains(cookie, "sid=") || !strings.Contains(cookie, "Max-Age=0") || !strings.Contains(cookie, "Path=/") {
		t.Fatalf("delete cookie = %q", cookie)
	}
}
