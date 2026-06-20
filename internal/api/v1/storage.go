package v1

import (
	"net/http"

	"github.com/radaiko/boxarr/internal/job"
	"github.com/radaiko/boxarr/internal/webdav"
)

// planSlots maps a TorBox plan tier to its concurrent active-download allowance
// (derived — /user/me does not return it; 00 §9 runtime-verify). Fallback 1.
var planSlots = map[int]int{0: 1, 1: 3, 2: 10, 3: 5}

var planNames = map[int]string{0: "Free", 1: "Essential", 2: "Pro", 3: "Standard"}

// storage reports total used bytes + TorBox plan/usage (FR-ST-1/2, FR-LIM-4).
func (h *Handler) storage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	used, _ := h.deps.Store.WebDAVUsageBytes(ctx)
	active, _ := h.deps.Store.CountJobsByState(ctx,
		job.StateSubmitting, job.StateQueued, job.StateDownloading, job.StateSeeding)

	resp := map[string]any{
		"usedBytes": used,
		"downloads": map[string]any{"active": active},
	}
	if h.deps.TorBox != nil {
		if u, err := h.deps.TorBox.UserMe(ctx); err == nil {
			tier := int(u.Plan)
			slots, ok := planSlots[tier]
			if !ok {
				slots = 1
			}
			resp["plan"] = map[string]any{
				"tier": tier, "tierName": planNames[tier], "concurrentSlots": slots,
				"isSubscribed": u.IsSubscribed, "premiumExpiresAt": u.PremiumExpiresAt,
			}
			resp["usage"] = map[string]any{
				"monthlyDownloadedBytes": u.TotalDownloaded,
				"cooldownUntil":          u.CooldownUntil,
				"inCooldown":             u.CooldownUntil != "",
			}
		} else {
			h.deps.Logger.Warn("storage: /user/me failed", "error", err)
		}
	}
	h.writeJSON(w, http.StatusOK, resp)
}

type webdavItemDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	RemotePath string `json:"remotePath"`
	Size       int64  `json:"size"`
	Category   string `json:"category"`
	Known      bool   `json:"known"`
	JobID      int64  `json:"jobId,omitempty"`
	IsBroken   bool   `json:"isBroken"`
	FirstSeen  string `json:"firstSeen"`
	LastSeen   string `json:"lastSeen"`
}

func toWebDAVDTO(it *webdav.WebDAVItem) webdavItemDTO {
	return webdavItemDTO{
		ID: it.ID, Name: it.Name, RemotePath: it.RemotePath, Size: it.Size,
		Category: it.Category, Known: it.Known, JobID: it.JobID, IsBroken: it.IsBroken,
		FirstSeen: rfc3339(it.FirstSeen), LastSeen: rfc3339(it.LastSeen),
	}
}

// listWebDAV lists mount items from the cached table (FR-WD-1/2; never scans the
// live mount per request — that is the reconciler's job, Phase 4).
func (h *Handler) listWebDAV(w http.ResponseWriter, r *http.Request) {
	items, err := h.deps.Store.ListWebDAVItems(r.Context())
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "internal", "listing webdav items")
		return
	}
	cat := r.URL.Query().Get("category")
	out := make([]webdavItemDTO, 0, len(items))
	for _, it := range items {
		if it.IsBroken && r.URL.Query().Get("includeBroken") != "true" {
			continue
		}
		if cat != "" && it.Category != cat {
			continue
		}
		out = append(out, toWebDAVDTO(it))
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"items": out, "total": len(out)})
}
