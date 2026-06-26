package worker

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// webdavRefreshBackoff is how long to stop calling /refresh after TorBox
// rate-limits it (HTTP 429). TorBox's own listing refreshes every 15 minutes
// regardless, so a 15-minute backoff costs nothing.
var webdavRefreshBackoff = 15 * time.Minute

// maybeRefreshWebDAV forces a TorBox WebDAV listing refresh by hitting the
// /refresh endpoint, so a completed download surfaces in seconds instead of
// waiting out TorBox's 15-minute refresh cycle.
//
// It is a no-op unless the WebDAV credentials are configured. pollOnce only
// calls it when every active download has finished, so it never fires while
// transfers are in progress. It is further debounced by the configured
// cooldown and backs off hard on an HTTP 429.
func (w *Workers) maybeRefreshWebDAV(ctx context.Context) {
	if !w.set.WebDAVRefreshEnabled() {
		return
	}
	now := time.Now()
	if now.Before(w.webdavBackoffUntil) {
		return
	}
	if !w.lastWebDAVRefresh.IsZero() && now.Sub(w.lastWebDAVRefresh) < w.set.WebDAVRefreshCooldown() {
		return
	}
	w.lastWebDAVRefresh = now

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.set.TorBoxWebDAVRefreshURL(), nil)
	if err != nil {
		w.logger.Error("building webdav refresh request", "error", err)
		return
	}
	req.SetBasicAuth(w.set.TorBoxWebDAVUser(), w.set.TorBoxWebDAVPass())

	resp, err := w.httpClient.Do(req)
	if err != nil {
		w.logger.Warn("webdav refresh request failed", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		w.webdavBackoffUntil = now.Add(webdavRefreshBackoff)
		w.logger.Warn("webdav refresh rate-limited; backing off",
			"backoff", webdavRefreshBackoff.String())
	case resp.StatusCode >= 400:
		snippet := ""
		if b, _ := io.ReadAll(io.LimitReader(resp.Body, 256)); len(b) > 0 {
			snippet = strings.TrimSpace(string(b))
		}
		w.logger.Warn("webdav refresh returned error status",
			"status", resp.StatusCode,
			"reason", webdavErrHint(resp.StatusCode),
			"credsConfigured", w.set.TorBoxWebDAVUser() != "" && w.set.TorBoxWebDAVPass() != "",
			"url", w.set.TorBoxWebDAVRefreshURL(),
			"body", snippet)
	default:
		w.logger.Info("forced torbox webdav refresh", "status", resp.StatusCode)
	}
}

// webdavErrHint maps a WebDAV refresh failure status to a likely cause the user
// can act on (the credentials are TorBox WebDAV user/pass, set in Settings).
func webdavErrHint(status int) string {
	switch status {
	case http.StatusUnauthorized: // 401
		return "TorBox WebDAV username/password rejected — invalid, empty, or expired. Check Settings → TorBox WebDAV credentials (the WebDAV password is your TorBox API key)."
	case http.StatusForbidden: // 403
		return "access forbidden — the WebDAV login is valid but not allowed to refresh; check your TorBox plan/permissions."
	case http.StatusNotFound: // 404
		return "refresh endpoint not found — check the TorBox WebDAV URL in Settings."
	default:
		return "unexpected status from the TorBox WebDAV endpoint."
	}
}
