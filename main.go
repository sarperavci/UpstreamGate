package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type Upstream struct {
	Raw string
	URL *url.URL
}

var (
	upstreamsMu sync.RWMutex
	upstreams   = map[string]*Upstream{}

	userConnsMu sync.Mutex
	userConns   = map[string][]net.Conn{} // active connections per user
)

// helper to register a connection for a user
func registerConn(user string, conn net.Conn) {
	userConnsMu.Lock()
	userConns[user] = append(userConns[user], conn)
	userConnsMu.Unlock()
}

// helper to close all active connections for a user
func closeUserConns(user string) {
	userConnsMu.Lock()
	conns := userConns[user]
	delete(userConns, user)
	userConnsMu.Unlock()
	for _, c := range conns {
		c.Close()
	}
}

// POST { "user":"u", "password":"p", "upstream":"socks5://host:port" }
func setUpstreamHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Upstream string `json:"upstream"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	u, err := url.Parse(req.Upstream)
	if err != nil {
		http.Error(w, "bad upstream url", http.StatusBadRequest)
		return
	}

	upstreamsMu.Lock()
	upstreams[req.User] = &Upstream{Raw: req.Upstream, URL: u}
	upstreamsMu.Unlock()

	// close any old connections for this user
	closeUserConns(req.User)

	w.WriteHeader(http.StatusNoContent)
}

func usernameFromRequest(r *http.Request) (string, error) {
	auth := r.Header.Get("Proxy-Authorization")
	if auth == "" {
		return "", errors.New("no auth")
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || strings.ToLower(parts[0]) != "basic" {
		return "", errors.New("unsupported auth")
	}
	b, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	user := strings.SplitN(string(b), ":", 2)[0]
	return user, nil
}

func pickUpstreamFor(r *http.Request) *Upstream {
	user, _ := usernameFromRequest(r)
	upstreamsMu.RLock()
	defer upstreamsMu.RUnlock()
	if u, ok := upstreams[user]; ok {
		return u
	}
	return &Upstream{Raw: "direct", URL: &url.URL{Scheme: "direct"}}
}

func dialerFor(up *Upstream) (proxy.Dialer, error) {
	if up == nil || up.URL == nil || up.URL.Scheme == "direct" {
		return proxy.FromEnvironment(), nil
	}

	switch up.URL.Scheme {
	case "socks5":
		var auth *proxy.Auth
		if up.URL.User != nil {
			pwd, _ := up.URL.User.Password()
			auth = &proxy.Auth{User: up.URL.User.Username(), Password: pwd}
		}
		return proxy.SOCKS5("tcp", up.URL.Host, auth, proxy.Direct)
	case "http", "https":
		return &httpConnectDialer{upstreamURL: up.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", up.URL.Scheme)
	}
}

// httpConnectDialer implements proxy.Dialer for HTTP proxies
type httpConnectDialer struct {
	upstreamURL *url.URL
}

func (d *httpConnectDialer) Dial(network, addr string) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", d.upstreamURL.Host, 10*time.Second)
	if err != nil {
		return nil, err
	}

	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", addr, addr)
	if d.upstreamURL.User != nil {
		user := d.upstreamURL.User.Username()
		pass, _ := d.upstreamURL.User.Password()
		b := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		req += "Proxy-Authorization: Basic " + b + "\r\n"
	}
	req += "\r\n"

	if _, err = conn.Write([]byte(req)); err != nil {
		conn.Close()
		return nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("proxy connect failed: %s", resp.Status)
	}

	return conn, nil
}

// relay raw bytes both ways
func relay(a, b net.Conn) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(a, b); done <- struct{}{} }()
	go func() { io.Copy(b, a); done <- struct{}{} }()
	<-done
}

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	user, err := usernameFromRequest(r)
	if err != nil {
		w.Header().Set("Proxy-Authenticate", "Basic realm=\"proxy\"")
		w.WriteHeader(http.StatusProxyAuthRequired)
		return
	}

	up := pickUpstreamFor(r)
	dialer, err := dialerFor(up)
	if err != nil {
		http.Error(w, "invalid upstream", http.StatusInternalServerError)
		return
	}

	if r.Method != http.MethodConnect {
		http.Error(w, "only CONNECT supported for raw tcp", http.StatusBadRequest)
		return
	}

	hij, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hij.Hijack()
	if err != nil {
		return
	}

	// register connection so it can be closed if upstream changes
	registerConn(user, clientConn)

	targetConn, err := dialer.Dial("tcp", r.Host)
	if err != nil {
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		clientConn.Close()
		return
	}

	clientConn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	relay(clientConn, targetConn)
}

func main() {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/upstream" {
			setUpstreamHandler(w, r)
			return
		}
		proxyHandler(w, r)
	})

	log.Println("proxy listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", handler))
}
