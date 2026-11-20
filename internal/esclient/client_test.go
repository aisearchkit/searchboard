package esclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewValidatesBaseURL(t *testing.T) {
	for _, baseURL := range []string{"", "localhost:9200", "ftp://es:9200", "http://"} {
		if _, err := New(Config{BaseURL: baseURL}); err == nil {
			t.Errorf("New(%q) succeeded, want error", baseURL)
		}
	}
	if _, err := New(Config{BaseURL: "https://es.example.com:9200"}); err != nil {
		t.Errorf("valid URL rejected: %v", err)
	}
}

func TestVersionDetection(t *testing.T) {
	tests := []struct {
		name         string
		distribution string
		number       string
		wantFlavor   Flavor
		wantMajor    int
	}{
		{"elasticsearch 5", "", "5.6.16", FlavorElasticsearch, 5},
		{"elasticsearch 7", "", "7.10.2", FlavorElasticsearch, 7},
		{"elasticsearch 8", "", "8.15.0", FlavorElasticsearch, 8},
		{"opensearch 2", "opensearch", "2.11.1", FlavorOpenSearch, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newFakeCluster(tt.distribution, tt.number, nil).start(t, Config{})
			got, err := client.Version(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.Flavor != tt.wantFlavor || got.Major != tt.wantMajor {
				t.Fatalf("got %s, want %s major %d", got, tt.wantFlavor, tt.wantMajor)
			}
		})
	}
}

func TestVersionIsCached(t *testing.T) {
	fake := newFakeCluster("", "7.17.0", nil)
	client := fake.start(t, Config{})

	for i := 0; i < 3; i++ {
		if _, err := client.Version(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.rootRequests(); got != 1 {
		t.Fatalf("root endpoint hit %d times, want 1", got)
	}
}

func TestVersionRejectsGarbage(t *testing.T) {
	client := newFakeCluster("", "not-a-version", nil).start(t, Config{})
	if _, err := client.Version(context.Background()); err == nil {
		t.Fatal("expected an error for an unparsable version")
	}
}

func TestAuthentication(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		fake := newFakeCluster("", "7.10.0", map[string]string{"/_cluster/health": `{}`})
		client := fake.start(t, Config{Username: "elastic", Password: "secret"})
		if _, err := client.Health(context.Background()); err != nil {
			t.Fatal(err)
		}
		user, pass, ok := (&http.Request{Header: fake.header()}).BasicAuth()
		if !ok || user != "elastic" || pass != "secret" {
			t.Fatalf("basic auth = %q/%q ok=%v", user, pass, ok)
		}
	})

	t.Run("api key wins over basic", func(t *testing.T) {
		fake := newFakeCluster("", "7.10.0", map[string]string{"/_cluster/health": `{}`})
		client := fake.start(t, Config{Username: "elastic", Password: "secret", APIKey: "abc123"})
		if _, err := client.Health(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := fake.header().Get("Authorization"); got != "ApiKey abc123" {
			t.Fatalf("Authorization = %q", got)
		}
	})
}

func TestBasePathPrefixIsPreserved(t *testing.T) {
	fake := newFakeCluster("", "7.10.0", map[string]string{"/es/_cluster/health": `{"status":"green"}`})
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	client, err := New(Config{BaseURL: server.URL + "/es/"})
	if err != nil {
		t.Fatal(err)
	}
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "green" {
		t.Fatalf("status = %q", health.Status)
	}
}

func TestAPIError(t *testing.T) {
	fake := newFakeCluster("", "7.10.0", map[string]string{
		"/_cluster/health": `!401 {"error":"security_exception"}`,
	})
	client := fake.start(t, Config{})

	_, err := client.Health(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not an *APIError", err)
	}
	if apiErr.Status != http.StatusUnauthorized || !HasStatus(err, 401) || HasStatus(err, 404) {
		t.Fatalf("unexpected status handling: %+v", apiErr)
	}
}

func TestContextCancellation(t *testing.T) {
	block := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-block }))
	t.Cleanup(func() { close(block); server.Close() })

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := client.Version(ctx); err == nil {
		t.Fatal("expected a context error")
	}
}
