package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/s-ui/util/common"

	"github.com/gin-gonic/gin"
)

// Actions that always run on the central panel itself, never proxied to a
// remote: the server registry, authentication and token management.
var remoteLocalOnly = map[string]bool{
	"servers":     true,
	"testServer":  true,
	"login":       true,
	"logout":      true,
	"tokens":      true,
	"addToken":    true,
	"deleteToken": true,
	"changePass":  true,
}

// remoteMiddleware forwards a request to a remote server's APIv2 (token auth)
// when it carries an X-Remote-Server header, so the central panel can manage
// other s-ui instances with the same UI. Local-only actions are never proxied.
func (a *ApiService) remoteMiddleware(c *gin.Context) {
	serverId := c.GetHeader("X-Remote-Server")
	if serverId == "" {
		return
	}
	action := path.Base(c.Request.URL.Path)
	if remoteLocalOnly[action] {
		return
	}
	a.proxyToRemote(c, serverId, action)
	c.Abort()
}

func (a *ApiService) proxyToRemote(c *gin.Context, serverId string, action string) {
	server, err := a.ServerListService.GetById(serverId)
	if err != nil || server.Url == "" {
		jsonMsg(c, "", common.NewError("remote server not found"))
		return
	}

	base := server.Url
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	target := base + "apiv2/" + action
	if c.Request.URL.RawQuery != "" {
		target += "?" + c.Request.URL.RawQuery
	}

	var body io.Reader
	if c.Request.Method == http.MethodPost {
		b, _ := io.ReadAll(c.Request.Body)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(c.Request.Method, target, body)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	req.Header.Set("Token", server.Token)
	if ct := c.Request.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
	}
	c.Data(resp.StatusCode, contentType, respBody)
}

// TestServer probes a registered remote server by calling its APIv2 status
// endpoint with the stored token, measuring round-trip latency. It returns
// {online, latency, error} so the UI can confirm the server link actually works.
func (a *ApiService) TestServer(c *gin.Context) {
	id := c.Query("id")
	server, err := a.ServerListService.GetById(id)
	if err != nil || server == nil || server.Url == "" {
		jsonObj(c, gin.H{"online": false, "error": "server not found"}, nil)
		return
	}

	base := server.Url
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	req, err := http.NewRequest(http.MethodGet, base+"apiv2/status", nil)
	if err != nil {
		jsonObj(c, gin.H{"online": false, "error": err.Error()}, nil)
		return
	}
	req.Header.Set("Token", server.Token)

	client := &http.Client{Timeout: 8 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		jsonObj(c, gin.H{"online": false, "error": "unreachable"}, nil)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		jsonObj(c, gin.H{"online": false, "latency": latency, "error": "http " + strconv.Itoa(resp.StatusCode)}, nil)
		return
	}

	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Success bool `json:"success"`
	}
	_ = json.Unmarshal(body, &parsed)
	if !parsed.Success {
		jsonObj(c, gin.H{"online": false, "latency": latency, "error": "bad token"}, nil)
		return
	}

	jsonObj(c, gin.H{"online": true, "latency": latency}, nil)
}
