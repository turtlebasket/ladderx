//go:build integration

package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const defaultLiveTestURL = "https://www.google.com/robots.txt"

func TestLiveExternalEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("live external integration test")
	}

	targetURL := os.Getenv("LIVE_TEST_URL")
	if targetURL == "" {
		targetURL = defaultLiveTestURL
	}

	parsedTarget, err := url.ParseRequestURI(targetURL)
	require.NoError(t, err, "LIVE_TEST_URL must be a valid absolute URL")
	require.NotEmpty(t, parsedTarget.Scheme, "LIVE_TEST_URL must include a scheme")
	require.NotEmpty(t, parsedTarget.Host, "LIVE_TEST_URL must include a host")

	resetHandlerTestGlobals(t)
	defaultTimeout = 15

	app := fiber.New()
	app.Get("/raw/*", Raw)
	app.Get("/api/*", Api)
	app.Get("/*", ProxySite(""))

	t.Run("raw", func(t *testing.T) {
		resp := performLiveRequest(t, app, "/raw/"+targetURL)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		assert.NotEmpty(t, strings.TrimSpace(string(body)))
	})

	t.Run("api", func(t *testing.T) {
		resp := performLiveRequest(t, app, "/api/"+targetURL)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))

		var payload Response
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.NotEmpty(t, strings.TrimSpace(payload.Body))
		assert.NotEmpty(t, payload.Request.Headers)
		assert.NotEmpty(t, payload.Response.Headers)
	})

	t.Run("proxy", func(t *testing.T) {
		resp := performLiveRequest(t, app, "/"+targetURL)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		assert.NotEmpty(t, resp.Header.Get("Content-Type"))
		assert.NotEmpty(t, strings.TrimSpace(string(body)))
	})
}

func performLiveRequest(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp, err := app.Test(req, int((20 * time.Second).Milliseconds()))
	require.NoError(t, err)

	return resp
}
