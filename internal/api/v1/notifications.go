package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

type notificationDTO struct {
	ID        int64          `json:"id"`
	Type      string         `json:"type"`
	Read      bool           `json:"read"`
	CreatedAt string         `json:"createdAt"`
	ReadAt    string         `json:"readAt,omitempty"`
	JobID     int64          `json:"jobId,omitempty"`
	Payload   map[string]any `json:"payload"`
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	unreadOnly := r.URL.Query().Get("unreadOnly") == "true"
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	notes, err := h.deps.Store.ListNotifications(ctx, unreadOnly, limit)
	if err != nil {
		h.serverError(w, "listing notifications", err)
		return
	}
	unread, _ := h.deps.Store.UnreadCount(ctx)
	items := make([]notificationDTO, 0, len(notes))
	for _, n := range notes {
		var payload map[string]any
		_ = json.Unmarshal([]byte(n.Payload), &payload)
		dto := notificationDTO{
			ID: n.ID, Type: n.Type, Read: n.Read, CreatedAt: rfc3339(n.CreatedAt),
			JobID: n.JobID, Payload: payload,
		}
		if n.ReadAt != nil {
			dto.ReadAt = rfc3339(*n.ReadAt)
		}
		items = append(items, dto)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": len(items), "unreadCount": unread,
	})
}

func (h *Handler) unreadCount(w http.ResponseWriter, r *http.Request) {
	n, err := h.deps.Store.UnreadCount(r.Context())
	if err != nil {
		h.serverError(w, "counting unread", err)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"unreadCount": n})
}

func (h *Handler) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	if err := h.deps.Store.MarkNotificationRead(r.Context(), id); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "marking read")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "read": true})
}

func (h *Handler) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	if err := h.deps.Store.MarkAllNotificationsRead(r.Context()); err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "marking all read")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// notificationAction resolves an unknown_content notification (FR-NC-2):
//   - ignore / adopt: mark the mount item known so it stops being flagged
//   - delete: delete the download from TorBox + drop the mount-item row
func (h *Handler) notificationAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	ctx := r.Context()
	n, err := h.deps.Store.GetNotification(ctx, id)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "not_found", "notification not found")
		return
	}
	if n.Type != "unknown_content" {
		h.writeError(w, http.StatusBadRequest, "bad_request", "action only valid for unknown_content")
		return
	}
	var p struct {
		RemotePath string `json:"remotePath"`
		Name       string `json:"name"`
	}
	_ = json.Unmarshal([]byte(n.Payload), &p)

	switch body.Action {
	case "adopt":
		// Import the existing content into the library: TMDB match → catalog row →
		// symlink → mark available. SetWebDAVItemKnown happens inside AdoptUnknown.
		if h.deps.Adopter == nil {
			h.writeError(w, http.StatusServiceUnavailable, "unavailable", "adopt not wired")
			return
		}
		if err := h.deps.Adopter.AdoptUnknown(ctx, p.RemotePath, p.Name, ""); err != nil {
			h.writeError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error())
			return
		}
	case "ignore":
		// Keep the content but stop flagging it (no catalog entry).
		if err := h.deps.Store.SetWebDAVItemKnown(ctx, p.RemotePath, true); err != nil {
			h.writeError(w, http.StatusInternalServerError, "internal", "updating item")
			return
		}
	case "delete":
		// Delete via the TorBox API when the download is in a mylist, AND remove
		// the folder from the WebDAV mount so it doesn't reappear on the next
		// reconcile (covers content present on the mount but not in mylist).
		h.deleteUnknownFromTorBox(ctx, p.Name)
		h.removeMountFolder(p.RemotePath)
		if err := h.deps.Store.DeleteWebDAVItemByPath(ctx, p.RemotePath); err != nil {
			h.writeError(w, http.StatusInternalServerError, "internal", "removing item")
			return
		}
	default:
		h.writeError(w, http.StatusBadRequest, "bad_request", "action must be adopt|ignore|delete")
		return
	}
	_ = h.deps.Store.MarkNotificationRead(ctx, id)
	h.writeJSON(w, http.StatusOK, map[string]any{"id": id, "action": body.Action, "ok": true})
}

// deleteUnknownFromTorBox best-effort deletes a download from TorBox by matching
// its folder name (case-insensitively) against the torrent then usenet mylists.
// Returns whether a matching download was found and a delete issued.
func (h *Handler) deleteUnknownFromTorBox(ctx context.Context, name string) bool {
	tb := h.deps.Settings.TorBox()
	torrents, terr := tb.ListTorrents(ctx)
	for _, d := range torrents {
		if strings.EqualFold(d.Name, name) {
			if derr := tb.ControlTorrent(ctx, int64(d.ID), "delete"); derr != nil {
				h.deps.Logger.Warn("unknown-content delete (torrent)", "name", name, "error", derr)
			}
			return true
		}
	}
	usenet, uerr := tb.ListUsenet(ctx)
	for _, d := range usenet {
		if strings.EqualFold(d.Name, name) {
			if derr := tb.ControlUsenet(ctx, int64(d.ID), "delete"); derr != nil {
				h.deps.Logger.Warn("unknown-content delete (usenet)", "name", name, "error", derr)
			}
			return true
		}
	}
	if terr != nil || uerr != nil {
		h.deps.Logger.Warn("unknown-content delete: could not query TorBox mylists",
			"name", name, "torrents_error", terr, "usenet_error", uerr)
	} else {
		// Expected for content TorBox already removed (stale rclone cache) or added
		// outside Boxarr — the folder removal below still clears it.
		h.deps.Logger.Info("unknown-content delete: not in a TorBox mylist; removing the mount folder instead", "name", name)
	}
	return false
}

// removeMountFolder deletes a release folder from the WebDAV mount (best-effort),
// guarded so it can only touch paths inside the configured mount root. On the
// rclone-backed mount this removes the content from TorBox's WebDAV.
func (h *Handler) removeMountFolder(remotePath string) {
	if remotePath == "" {
		return
	}
	root := h.deps.Settings.WebDAVMountRoot()
	if root == "" {
		h.deps.Logger.Warn("delete: WebDAV mount root not configured — folder left on the mount", "path", remotePath)
		return
	}
	clean := filepath.Clean(remotePath)
	rel, err := filepath.Rel(filepath.Clean(root), clean)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") {
		h.deps.Logger.Warn("delete: path outside mount root, refusing", "path", remotePath)
		return
	}
	if err := os.RemoveAll(clean); err != nil {
		h.deps.Logger.Warn("unknown-content delete: removing mount folder", "path", clean, "error", err)
	}
}
