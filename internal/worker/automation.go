package worker

import "context"

// autoSearchOnce runs one scheduled auto-search of wanted items (FR-SR-4). It is
// gated on the live AutomationEnabled() setting, so the UI toggle takes effect
// without a restart.
func (w *Workers) autoSearchOnce(ctx context.Context) error {
	if w.automation == nil || !w.set.AutomationEnabled() {
		return nil
	}
	if err := w.automation.AutoSearchWanted(ctx); err != nil {
		return err
	}
	// Also look to upgrade already-imported items to a better language/quality,
	// unless the upgrade toggle is off.
	if w.set.UpgradeEnabled() {
		return w.automation.UpgradeWanted(ctx)
	}
	return nil
}

// metadataRefreshOnce runs one scheduled metadata refresh (FR-CAT-5), gated on
// the live AutomationEnabled() setting.
func (w *Workers) metadataRefreshOnce(ctx context.Context) error {
	if w.automation == nil || !w.set.AutomationEnabled() {
		return nil
	}
	return w.automation.RefreshMetadata(ctx)
}
