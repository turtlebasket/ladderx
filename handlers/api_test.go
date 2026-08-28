package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI(t *testing.T) {
	resetHandlerTestGlobals(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Upstream", "fixture")
		if _, err := w.Write([]byte("fixture body")); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	app := fiber.New()
	app.Get("/api/*", Api)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "upstream response",
			url:            upstream.URL,
			expectedStatus: http.StatusOK,
			expectedBody:   "fixture body",
		},
		{
			name:           "invalid url",
			url:            "invalid-url",
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/"+tt.url, nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, tt.expectedStatus, resp.StatusCode)

			if tt.expectedBody != "" {
				var payload Response
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&payload))
				assert.Equal(t, tt.expectedBody, payload.Body)
				assert.Equal(t, version, payload.Version)
				return
			}

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), "unsupported protocol scheme")
		})
	}
}
