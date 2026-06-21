package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

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
		h.writeError(w, http.StatusInternalServerError, "internal", "listing notifications")
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
		h.writeError(w, http.StatusInternalServerError, "internal", "counting unread")
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
	case "ignore", "adopt":
		// Keep the content; stop flagging it. (Full library adoption — TMDB match
		// + catalog link — is not yet implemented; this marks it accounted-for.)
		if err := h.deps.Store.SetWebDAVItemKnown(ctx, p.RemotePath, true); err != nil {
			h.writeError(w, http.StatusInternalServerError, "internal", "updating item")
			return
		}
	case "delete":
		h.deleteUnknownFromTorBox(ctx, p.Name)
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

// deleteUnknownFromTorBox best-effort deletes a download (matched by folder name)
// from TorBox, checking torrents then usenet.
func (h *Handler) deleteUnknownFromTorBox(ctx context.Context, name string) {
	tb := h.deps.Settings.TorBox()
	if torrents, err := tb.ListTorrents(ctx); err == nil {
		for _, d := range torrents {
			if d.Name == name {
				if derr := tb.ControlTorrent(ctx, int64(d.ID), "delete"); derr != nil {
					h.deps.Logger.Warn("unknown-content delete (torrent)", "name", name, "error", derr)
				}
				return
			}
		}
	}
	if usenet, err := tb.ListUsenet(ctx); err == nil {
		for _, d := range usenet {
			if d.Name == name {
				if derr := tb.ControlUsenet(ctx, int64(d.ID), "delete"); derr != nil {
					h.deps.Logger.Warn("unknown-content delete (usenet)", "name", name, "error", derr)
				}
				return
			}
		}
	}
	h.deps.Logger.Warn("unknown-content delete: no matching TorBox download", "name", name)
}
