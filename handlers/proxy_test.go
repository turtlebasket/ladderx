package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"ladder/pkg/ruleset"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxySite(t *testing.T) {
	resetHandlerTestGlobals(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/article", r.URL.Path)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte("proxied response")); err != nil {
			t.Errorf("write fixture response: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	app := fiber.New()
	app.Get("/*", ProxySite(""))

	req := httptest.NewRequest(http.MethodGet, "/"+upstream.URL+"/article", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "proxied response", string(body))
}

func TestRewriteHtml(t *testing.T) {
	resetHandlerTestGlobals(t)

	bodyB := []byte(`
		<html>
			<head>
				<title>Test Page</title>
			</head>
			<body>
				<img src="/image.jpg">
				<script src="/script.js"></script>
				<a href="/about">About Us</a>
				<div style="background-image: url('/background.jpg')"></div>
			</body>
		</html>
	`)
	u := &url.URL{Scheme: "https", Host: "example.com"}

	expected := `
		<html>
			<head>
				<title>Test Page</title>
			</head>
			<body>
				<img src="/https://example.com/image.jpg">
				<script src="/https://example.com/script.js"></script>
				<a href="/https://example.com/about">About Us</a>
				<div style="background-image: url('/https://example.com/background.jpg')"></div>
			</body>
		</html>
	`

	actual := rewriteHtml(bodyB, u, ruleset.Rule{})
	assert.Equal(t, expected, actual)
}

func resetHandlerTestGlobals(t *testing.T) {
	t.Helper()

	originalAllowedDomains := allowedDomains
	originalRulesSet := rulesSet
	originalFlareSolverrHost := flareSolverrHost
	originalDefaultTimeout := defaultTimeout
	originalBasePath := basePath

	allowedDomains = []string{""}
	rulesSet = nil
	flareSolverrHost = ""
	defaultTimeout = 5
	basePath = ""

	t.Cleanup(func() {
		allowedDomains = originalAllowedDomains
		rulesSet = originalRulesSet
		flareSolverrHost = originalFlareSolverrHost
		defaultTimeout = originalDefaultTimeout
		basePath = originalBasePath
	})
}
