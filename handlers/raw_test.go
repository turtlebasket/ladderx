package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestRaw(t *testing.T) {
	resetHandlerTestGlobals(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("<!doctype html><title>fixture</title>")); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	app := fiber.New()
	app.Get("/raw/*", Raw)

	testCases := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "upstream response",
			url:      upstream.URL,
			expected: "<!doctype html><title>fixture</title>",
		},
		{
			name:     "invalid url",
			url:      "invalid-url",
			expected: "unsupported protocol scheme",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/raw/"+tc.url, nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer resp.Body.Close()

			expectedStatus := http.StatusOK
			if tc.url == "invalid-url" {
				expectedStatus = http.StatusInternalServerError
			}
			if resp.StatusCode != expectedStatus {
				t.Errorf("expected status %d; got %v", expectedStatus, resp.Status)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.Contains(string(body), tc.expected) {
				t.Errorf("expected body to contain %q; got %q", tc.expected, string(body))
			}
		})
	}
}
