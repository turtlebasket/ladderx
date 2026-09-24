package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func newTargetTransport(proxyAddress string) (http.RoundTripper, error) {
	proxyAddress = strings.TrimSpace(proxyAddress)
	if proxyAddress == "" {
		return http.DefaultTransport, nil
	}

	if !strings.Contains(proxyAddress, "://") {
		proxyAddress = "socks5://" + proxyAddress
	}

	proxyURL, err := url.Parse(proxyAddress)
	if err != nil {
		return nil, fmt.Errorf("invalid SOCKS5_PROXY: expected socks5://[user:password@]host[:port]")
	}

	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	if proxyURL.Scheme != "socks5" && proxyURL.Scheme != "socks5h" {
		return nil, fmt.Errorf("invalid SOCKS5_PROXY scheme %q: expected socks5 or socks5h", proxyURL.Scheme)
	}
	if proxyURL.Hostname() == "" {
		return nil, fmt.Errorf("invalid SOCKS5_PROXY: host is required")
	}
	if proxyURL.Path != "" && proxyURL.Path != "/" {
		return nil, fmt.Errorf("invalid SOCKS5_PROXY: paths are not supported")
	}
	if proxyURL.RawQuery != "" || proxyURL.Fragment != "" {
		return nil, fmt.Errorf("invalid SOCKS5_PROXY: query strings and fragments are not supported")
	}
	if port := proxyURL.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return nil, fmt.Errorf("invalid SOCKS5_PROXY: port must be between 1 and 65535")
		}
	}

	forwardDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	proxyDialer, err := xproxy.FromURL(proxyURL, forwardDialer)
	if err != nil {
		return nil, fmt.Errorf("invalid SOCKS5_PROXY configuration: %w", err)
	}
	contextDialer, ok := proxyDialer.(xproxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("SOCKS5_PROXY dialer does not support request cancellation")
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("configure SOCKS5_PROXY: unsupported default HTTP transport")
	}

	transport := defaultTransport.Clone()
	transport.Proxy = nil
	transport.DialContext = contextDialer.DialContext

	return transport, nil
}
