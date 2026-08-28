package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ladder/pkg/ruleset"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProxyReference(t *testing.T) {
	previousBasePath := basePath
	basePath = "/proxy"
	t.Cleanup(func() { basePath = previousBasePath })

	pageURL, err := url.Parse("https://example.com/articles/current?old=1")
	require.NoError(t, err)

	tests := []struct {
		name      string
		reference string
		want      string
		rewritten bool
	}{
		{name: "root relative", reference: "/mypage", want: "/proxy/https://example.com/mypage", rewritten: true},
		{name: "path relative", reference: "next", want: "/proxy/https://example.com/articles/next", rewritten: true},
		{name: "parent relative", reference: "../archive", want: "/proxy/https://example.com/archive", rewritten: true},
		{name: "absolute same host", reference: "https://example.com/mypage", want: "/proxy/https://example.com/mypage", rewritten: true},
		{name: "absolute same host different scheme", reference: "http://example.com/mypage", want: "/proxy/http://example.com/mypage", rewritten: true},
		{name: "scheme relative same host", reference: "//example.com/mypage", want: "/proxy/https://example.com/mypage", rewritten: true},
		{name: "query", reference: "?new=2", want: "/proxy/https://example.com/articles/current?new=2", rewritten: true},
		{name: "fragment only", reference: "#details", want: "#details", rewritten: false},
		{name: "external absolute", reference: "https://other.example/mypage", want: "https://other.example/mypage", rewritten: false},
		{name: "external scheme relative", reference: "//other.example/mypage", want: "//other.example/mypage", rewritten: false},
		{name: "mailto", reference: "mailto:person@example.com", want: "mailto:person@example.com", rewritten: false},
		{name: "data URL", reference: "data:image/gif;base64,AAAA", want: "data:image/gif;base64,AAAA", rewritten: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rewritten := resolveProxyReference(pageURL, tt.reference)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.rewritten, rewritten)
		})
	}
}

func TestRewriteHtmlResolvesURLAttributes(t *testing.T) {
	previousBasePath := basePath
	basePath = "/proxy"
	t.Cleanup(func() { basePath = previousBasePath })

	pageURL, err := url.Parse("https://example.com/articles/current")
	require.NoError(t, err)

	body := []byte(`<html><head><link href="/site.css"></head><body>
		<a id="absolute" href="https://example.com/mypage">absolute</a>
		<a id="relative" href='next'>relative</a>
		<a id="fragment" href="#details">fragment</a>
		<a id="external" href="https://other.example/page">external</a>
		<img src="/image.jpg">
		<script src="/script.js"></script>
		<div style="background-image: url('/background.jpg')"></div>
	</body></html>`)

	got := rewriteHtml(body, pageURL, ruleset.Rule{})

	assert.Contains(t, got, `href="/proxy/https://example.com/mypage"`)
	assert.NotContains(t, got, `example.com//mypage`)
	assert.Contains(t, got, `href="/proxy/https://example.com/articles/next"`)
	assert.Contains(t, got, `href="#details"`)
	assert.Contains(t, got, `href="https://other.example/page"`)
	assert.Contains(t, got, `href="/proxy/https://example.com/site.css"`)
	assert.Contains(t, got, `src="/proxy/https://example.com/image.jpg"`)
	assert.Contains(t, got, `src="/proxy/https://example.com/script.js"`)
	assert.Contains(t, got, `url('/proxy/https://example.com/background.jpg')`)
}

func TestFetchSiteRewritesAgainstFinalRedirectURL(t *testing.T) {
	previousBasePath := basePath
	basePath = ""
	t.Cleanup(func() { basePath = previousBasePath })

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/articles/current", http.StatusFound)
		case "/articles/current":
			_, _ = fmt.Fprint(w, `<a href="next">next</a>`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	body, _, response, err := fetchSite(upstream.URL+"/start", map[string]string{})
	require.NoError(t, err)
	require.NotNil(t, response)

	finalURL := response.Request.URL
	want := `/` + finalURL.Scheme + `://` + finalURL.Host + `/articles/next`
	assert.Contains(t, body, `href="`+want+`"`)
	assert.False(t, strings.Contains(body, finalURL.Host+`//articles/next`))
}
