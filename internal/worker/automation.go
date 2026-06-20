package worker

import "context"

// autoSearchOnce runs one scheduled auto-search of wanted items (FR-SR-4).
func (w *Workers) autoSearchOnce(ctx context.Context) error {
	if w.automation == nil {
		return nil
	}
	return w.automation.AutoSearchWanted(ctx)
}

// metadataRefreshOnce runs one scheduled metadata refresh (FR-CAT-5).
func (w *Workers) metadataRefreshOnce(ctx context.Context) error {
	if w.automation == nil {
		return nil
	}
	return w.automation.RefreshMetadata(ctx)
}
