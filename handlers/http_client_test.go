package handlers

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTargetTransport(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{name: "without authentication"},
		{name: "with authentication", username: "proxy-user", password: "proxy-password"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte("proxied through SOCKS5"))
				require.NoError(t, err)
			}))
			t.Cleanup(upstream.Close)

			proxyAddress, result := startSOCKS5TestProxy(t, tt.username, tt.password)
			proxyConfig := proxyAddress
			if tt.username != "" {
				proxyURL := &url.URL{
					Scheme: "socks5",
					Host:   proxyAddress,
					User:   url.UserPassword(tt.username, tt.password),
				}
				proxyConfig = proxyURL.String()
			}

			transport, err := newTargetTransport(proxyConfig)
			require.NoError(t, err)

			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			req, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
			require.NoError(t, err)
			req.Close = true

			resp, err := client.Do(req)
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			assert.Equal(t, "proxied through SOCKS5", string(body))

			proxyResult := <-result
			require.NoError(t, proxyResult.err)

			upstreamURL, err := url.Parse(upstream.URL)
			require.NoError(t, err)
			assert.Equal(t, upstreamURL.Host, proxyResult.destination)
		})
	}
}

func TestNewTargetTransportRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		proxy       string
		errorString string
	}{
		{name: "unsupported scheme", proxy: "http://localhost:1080", errorString: "expected socks5 or socks5h"},
		{name: "missing host", proxy: "socks5://", errorString: "host is required"},
		{name: "invalid port", proxy: "socks5://localhost:70000", errorString: "port must be between 1 and 65535"},
		{name: "path", proxy: "socks5://localhost:1080/proxy", errorString: "paths are not supported"},
		{name: "query", proxy: "socks5://localhost:1080?mode=test", errorString: "query strings and fragments are not supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTargetTransport(tt.proxy)
			require.ErrorContains(t, err, tt.errorString)
		})
	}
}

type socks5TestResult struct {
	destination string
	err         error
}

func startSOCKS5TestProxy(t *testing.T, username, password string) (string, <-chan socks5TestResult) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, listener.Close()) })

	result := make(chan socks5TestResult, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			result <- socks5TestResult{err: err}
			return
		}
		defer conn.Close()

		destination, err := handleSOCKS5TestConnection(conn, username, password)
		result <- socks5TestResult{destination: destination, err: err}
	}()

	return listener.Addr().String(), result
}

func handleSOCKS5TestConnection(client net.Conn, username, password string) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil {
		return "", err
	}
	if header[0] != 5 {
		return "", fmt.Errorf("unexpected SOCKS version %d", header[0])
	}

	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(client, methods); err != nil {
		return "", err
	}

	selectedMethod := byte(0)
	if username != "" {
		selectedMethod = 2
	}
	if !containsByte(methods, selectedMethod) {
		return "", fmt.Errorf("client did not offer authentication method %d", selectedMethod)
	}
	if _, err := client.Write([]byte{5, selectedMethod}); err != nil {
		return "", err
	}

	if selectedMethod == 2 {
		if err := authenticateSOCKS5TestClient(client, username, password); err != nil {
			return "", err
		}
	}

	requestHeader := make([]byte, 4)
	if _, err := io.ReadFull(client, requestHeader); err != nil {
		return "", err
	}
	if requestHeader[0] != 5 || requestHeader[1] != 1 {
		return "", fmt.Errorf("unexpected SOCKS5 request header %v", requestHeader)
	}

	host, err := readSOCKS5Host(client, requestHeader[3])
	if err != nil {
		return "", err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(client, portBytes); err != nil {
		return "", err
	}
	destination := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(portBytes))))

	upstream, err := net.DialTimeout("tcp", destination, 5*time.Second)
	if err != nil {
		_, _ = client.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return "", err
	}
	defer upstream.Close()

	if _, err := client.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return "", err
	}

	copyDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, client)
		close(copyDone)
	}()
	_, err = io.Copy(client, upstream)
	<-copyDone

	return destination, err
}

func authenticateSOCKS5TestClient(client net.Conn, expectedUsername, expectedPassword string) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(client, header); err != nil {
		return err
	}
	if header[0] != 1 {
		return fmt.Errorf("unexpected username/password auth version %d", header[0])
	}

	username := make([]byte, int(header[1]))
	if _, err := io.ReadFull(client, username); err != nil {
		return err
	}
	passwordLength := make([]byte, 1)
	if _, err := io.ReadFull(client, passwordLength); err != nil {
		return err
	}
	password := make([]byte, int(passwordLength[0]))
	if _, err := io.ReadFull(client, password); err != nil {
		return err
	}

	if string(username) != expectedUsername || string(password) != expectedPassword {
		_, _ = client.Write([]byte{1, 1})
		return fmt.Errorf("unexpected SOCKS5 credentials")
	}
	_, err := client.Write([]byte{1, 0})
	return err
}

func readSOCKS5Host(reader io.Reader, addressType byte) (string, error) {
	switch addressType {
	case 1:
		address := make([]byte, net.IPv4len)
		_, err := io.ReadFull(reader, address)
		return net.IP(address).String(), err
	case 3:
		length := make([]byte, 1)
		if _, err := io.ReadFull(reader, length); err != nil {
			return "", err
		}
		address := make([]byte, int(length[0]))
		_, err := io.ReadFull(reader, address)
		return string(address), err
	case 4:
		address := make([]byte, net.IPv6len)
		_, err := io.ReadFull(reader, address)
		return net.IP(address).String(), err
	default:
		return "", fmt.Errorf("unsupported SOCKS5 address type %d", addressType)
	}
}

func containsByte(values []byte, expected byte) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
