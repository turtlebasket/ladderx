package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
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

func TestDomainAllowed(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		domains []string
		want    bool
	}{
		{name: "exact domain", host: "example.com", domains: []string{"example.com"}, want: true},
		{name: "subdomain", host: "www.example.com", domains: []string{"example.com"}, want: true},
		{name: "case and trailing dot", host: "WWW.EXAMPLE.COM.", domains: []string{"example.com"}, want: true},
		{name: "whitespace in allowlist", host: "example.com", domains: []string{" example.com "}, want: true},
		{name: "hostname suffix attack", host: "notexample.com", domains: []string{"example.com"}, want: false},
		{name: "hostname prefix attack", host: "example.com.attacker.test", domains: []string{"example.com"}, want: false},
		{name: "empty entry", host: "example.com", domains: []string{""}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, domainAllowed(tt.host, tt.domains))
		})
	}
}

func TestFetchSiteRejectsDomainPrefixAttack(t *testing.T) {
	resetHandlerTestGlobals(t)
	allowedDomains = []string{"example.com"}

	_, _, _, err := fetchSite("https://example.com.attacker.test/article", nil)
	require.EqualError(t, err, "domain not allowed. example.com.attacker.test not in [example.com]")
}

func resetHandlerTestGlobals(t *testing.T) {
	t.Helper()

	originalAllowedDomains := allowedDomains
	originalRulesSet := rulesSet
	originalFlareSolverrHost := flareSolverrHost
	originalDefaultTimeout := defaultTimeout
	originalBasePath := basePath
	originalTargetTransport := targetTransport
	originalTargetTransportErr := targetTransportErr

	allowedDomains = nil
	rulesSet = nil
	flareSolverrHost = ""
	defaultTimeout = 5
	basePath = ""
	targetTransport = http.DefaultTransport
	targetTransportErr = nil

	t.Cleanup(func() {
		allowedDomains = originalAllowedDomains
		rulesSet = originalRulesSet
		flareSolverrHost = originalFlareSolverrHost
		defaultTimeout = originalDefaultTimeout
		basePath = originalBasePath
		targetTransport = originalTargetTransport
		targetTransportErr = originalTargetTransportErr
	})
}

func TestFetchSiteFlareSolverr(t *testing.T) {
	for _, tc := range []struct {
		name        string
		path        string
		solverFails bool
		wantSolver  bool
		wantDirect  bool
		wantBody    string
	}{
		{
			name:       "browser page without refetch",
			path:       "/article",
			wantSolver: true,
		},
		{
			name:       "static asset fetched directly",
			path:       "/app.js",
			wantDirect: true,
			wantBody:   "direct response",
		},
		{
			name:        "solver failure falls back to direct fetch",
			path:        "/article",
			solverFails: true,
			wantSolver:  true,
			wantDirect:  true,
			wantBody:    "direct response",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetHandlerTestGlobals(t)
			var directCalls, solverCalls atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				directCalls.Add(1)
				assert.Equal(t, tc.path, r.URL.Path)
				_, err := w.Write([]byte("direct response"))
				assert.NoError(t, err)
			}))
			t.Cleanup(target.Close)

			solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				solverCalls.Add(1)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/v1", r.URL.Path)
				var request FlareSolverrRequest
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
				assert.Equal(t, "request.get", request.Cmd)
				assert.Equal(t, target.URL+tc.path, request.URL)

				response := FlareSolverrResponse{Status: "ok"}
				response.Solution.Response = `<script src="/app.js"></script>`
				response.Solution.Status = http.StatusAccepted
				if tc.solverFails {
					response.Status = "error"
					response.Message = "fixture solver failure"
				}
				assert.NoError(t, json.NewEncoder(w).Encode(response))
			}))
			t.Cleanup(solver.Close)
			flareSolverrHost = solver.URL
			u, err := url.Parse(target.URL)
			require.NoError(t, err)
			rule := ruleset.Rule{Domain: u.Host, UseFlareSolverr: true}
			rule.Headers.CSP = "script-src 'self'"
			rulesSet = ruleset.RuleSet{rule}

			body, req, resp, err := fetchSite(target.URL+tc.path, nil)
			require.NoError(t, err)
			assert.Equal(t, target.URL+tc.path, req.URL.String())
			assert.Equal(t, rule.Headers.CSP, resp.Header.Get("Content-Security-Policy"))
			assert.Equal(t, tc.wantDirect, directCalls.Load() > 0)
			assert.Equal(t, tc.wantSolver, solverCalls.Load() > 0)
			if tc.wantDirect {
				assert.Equal(t, tc.wantBody, body)
			} else {
				assert.Equal(t, `<script src="/`+target.URL+`/app.js"></script>`, body)
				assert.Equal(t, http.StatusAccepted, resp.StatusCode)
				assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
				assert.Equal(t, int32(1), solverCalls.Load())
			}
		})
	}
}
