package api_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"devices-api/api"
)

func TestSwaggerUIAssets(t *testing.T) {
	server := httptest.NewServer(api.SwaggerUI())
	t.Cleanup(server.Close)

	tests := []struct {
		name        string
		path        string
		contentType string
		body        string
	}{
		{name: "index", path: "/", contentType: "text/html", body: "/openapi.yaml"},
		{name: "bundle", path: "/swagger-ui-bundle.js", contentType: "text/javascript", body: "SwaggerUIBundle"},
		{name: "stylesheet", path: "/swagger-ui.css", contentType: "text/css", body: ".swagger-ui"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(server.URL + tt.path)
			if err != nil {
				t.Fatalf("GET %s: %v", tt.path, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want %d", tt.path, resp.StatusCode, http.StatusOK)
			}
			if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, tt.contentType) {
				t.Errorf("Content-Type = %q, want prefix %q", got, tt.contentType)
			}
			if got := resp.Header.Get("Cache-Control"); got != "public, max-age=3600" {
				t.Errorf("Cache-Control = %q", got)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), tt.body) {
				t.Errorf("body does not contain %q", tt.body)
			}
		})
	}
}

func TestSwaggerUIMountRedirectsToTrailingSlash(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /docs/", http.StripPrefix("/docs/", api.SwaggerUI()))

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTemporaryRedirect)
	}
	if got := rec.Header().Get("Location"); got != "/docs/" {
		t.Errorf("Location = %q, want /docs/", got)
	}
}
