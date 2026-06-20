package v1

import (
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
