package worker

import "context"

// autoSearchOnce runs one scheduled auto-search of wanted items (FR-SR-4). It is
// gated on the live AutomationEnabled() setting, so the UI toggle takes effect
// without a restart.
func (w *Workers) autoSearchOnce(ctx context.Context) error {
	if w.automation == nil || !w.set.AutomationEnabled() {
		return nil
	}
	return w.automation.AutoSearchWanted(ctx)
}

// metadataRefreshOnce runs one scheduled metadata refresh (FR-CAT-5), gated on
// the live AutomationEnabled() setting.
func (w *Workers) metadataRefreshOnce(ctx context.Context) error {
	if w.automation == nil || !w.set.AutomationEnabled() {
		return nil
	}
	return w.automation.RefreshMetadata(ctx)
}
