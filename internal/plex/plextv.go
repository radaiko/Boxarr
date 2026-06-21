package plex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// plexTVBase is plex.tv's account API (PIN OAuth + resource discovery), distinct
// from a user's own Plex Media Server.
const plexTVBase = "https://plex.tv/api/v2"

// TV talks to plex.tv for the PIN-based OAuth login flow and server discovery.
// clientID is a stable per-install identifier (X-Plex-Client-Identifier).
type TV struct {
	clientID string
	product  string
	base     string
	http     *http.Client
}

// NewTV constructs a plex.tv client for the given stable client identifier.
func NewTV(clientID string) *TV {
	return &TV{clientID: clientID, product: "Boxarr", base: plexTVBase, http: &http.Client{Timeout: 20 * time.Second}}
}

// Pin is a plex.tv login PIN.
type Pin struct {
	ID   int64  `json:"id"`
	Code string `json:"code"`
}

// CreatePin starts a login: plex.tv returns an id + a short code the user
// authorizes at app.plex.tv/auth.
func (t *TV) CreatePin(ctx context.Context) (Pin, error) {
	var pin Pin
	body, err := t.do(ctx, http.MethodPost, t.base+"/pins?strong=true", "")
	if err != nil {
		return pin, err
	}
	if err := json.Unmarshal(body, &pin); err != nil {
		return pin, fmt.Errorf("decoding pin: %w", err)
	}
	return pin, nil
}

// CheckPin polls a PIN; authToken is non-empty once the user has authorized.
func (t *TV) CheckPin(ctx context.Context, id int64) (string, error) {
	body, err := t.do(ctx, http.MethodGet, fmt.Sprintf("%s/pins/%d", t.base, id), "")
	if err != nil {
		return "", err
	}
	var r struct {
		AuthToken string `json:"authToken"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("decoding pin status: %w", err)
	}
	return r.AuthToken, nil
}

// AuthURL is the app.plex.tv page the user opens to authorize a PIN.
func (t *TV) AuthURL(code string) string {
	q := url.Values{}
	q.Set("clientID", t.clientID)
	q.Set("code", code)
	q.Set("context[device][product]", t.product)
	return "https://app.plex.tv/auth#?" + q.Encode()
}

// Server is a discovered Plex Media Server with the connection URIs Boxarr could
// reach it on (best-guess default first).
type Server struct {
	Name string   `json:"name"`
	URI  string   `json:"uri"`
	URIs []string `json:"uris"`
}

// Servers lists the Plex Media Servers the account owns/has access to.
func (t *TV) Servers(ctx context.Context, token string) ([]Server, error) {
	body, err := t.do(ctx, http.MethodGet, t.base+"/resources?includeHttps=1&includeRelay=1", token)
	if err != nil {
		return nil, err
	}
	var devices []struct {
		Name        string `json:"name"`
		Provides    string `json:"provides"`
		Connections []struct {
			URI      string `json:"uri"`
			Local    bool   `json:"local"`
			Relay    bool   `json:"relay"`
			Protocol string `json:"protocol"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &devices); err != nil {
		return nil, fmt.Errorf("decoding resources: %w", err)
	}
	var out []Server
	for _, d := range devices {
		if !strings.Contains(d.Provides, "server") {
			continue
		}
		uris := make([]string, 0, len(d.Connections))
		for _, c := range d.Connections {
			if !c.Relay && c.URI != "" {
				uris = append(uris, c.URI)
			}
		}
		if len(uris) == 0 {
			continue
		}
		out = append(out, Server{Name: d.Name, URI: bestURI(d.Connections), URIs: uris})
	}
	return out, nil
}

// bestURI prefers a local (LAN) connection — reachable from a container on the
// same network and matching the user's typical setup — then any non-relay.
func bestURI(conns []struct {
	URI      string `json:"uri"`
	Local    bool   `json:"local"`
	Relay    bool   `json:"relay"`
	Protocol string `json:"protocol"`
}) string {
	for _, c := range conns {
		if c.Local && !c.Relay && c.URI != "" {
			return c.URI
		}
	}
	for _, c := range conns {
		if !c.Relay && c.URI != "" {
			return c.URI
		}
	}
	if len(conns) > 0 {
		return conns[0].URI
	}
	return ""
}

func (t *TV) do(ctx context.Context, method, u, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Plex-Product", t.product)
	req.Header.Set("X-Plex-Client-Identifier", t.clientID)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("plex.tv %s: status %d", u, resp.StatusCode)
	}
	return body, nil
}
