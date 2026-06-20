package store

import (
	"context"
	"testing"
)

func TestSettingsKVAndServarrSeed(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if _, ok, _ := st.GetSetting(ctx, "prowlarr.url"); ok {
		t.Fatal("unset key should report not-found")
	}
	if err := st.SetSetting(ctx, "prowlarr.url", "http://x:9696"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	if v, ok, _ := st.GetSetting(ctx, "prowlarr.url"); !ok || v != "http://x:9696" {
		t.Fatalf("GetSetting = %q,%v", v, ok)
	}
	// Overwrite via ON CONFLICT.
	if err := st.SetSetting(ctx, "prowlarr.url", "http://y:9696"); err != nil {
		t.Fatalf("SetSetting overwrite: %v", err)
	}
	all, _ := st.AllSettings(ctx)
	if all["prowlarr.url"] != "http://y:9696" {
		t.Fatalf("AllSettings overwrite: %v", all)
	}

	profs, _ := st.ListQualityProfiles(ctx)
	if len(profs) != 1 || profs[0].ID != 1 || profs[0].Name != "Any" || !profs[0].IsDefault {
		t.Fatalf("expected seeded profile id=1 'Any' default, got %+v", profs)
	}
	tvRoots, _ := st.ListRootFolders(ctx, "tv")
	if len(tvRoots) != 1 || tvRoots[0].ID != 1 || tvRoots[0].Path != "/data/tv" {
		t.Fatalf("expected seeded tv root id=1 /data/tv, got %+v", tvRoots)
	}
	movieRoots, _ := st.ListRootFolders(ctx, "movie")
	if len(movieRoots) != 1 || movieRoots[0].ID != 2 || movieRoots[0].Path != "/data/movies" {
		t.Fatalf("expected seeded movie root id=2 /data/movies, got %+v", movieRoots)
	}
	if allRoots, _ := st.ListRootFolders(ctx, ""); len(allRoots) != 2 {
		t.Fatalf("ListRootFolders(all) should return 2, got %d", len(allRoots))
	}
	if tags, _ := st.ListTags(ctx); len(tags) != 0 {
		t.Fatalf("tag table should seed empty, got %d", len(tags))
	}

	// Re-point a seeded root (settings UI) and create a tag lazily.
	if err := st.UpsertRootFolder(ctx, 1, "/data/tv2", "tv"); err != nil {
		t.Fatalf("UpsertRootFolder: %v", err)
	}
	if r, _ := st.ListRootFolders(ctx, "tv"); r[0].Path != "/data/tv2" {
		t.Fatalf("UpsertRootFolder should re-point id 1, got %q", r[0].Path)
	}
	if _, err := st.CreateTag(ctx, "hd"); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if tags, _ := st.ListTags(ctx); len(tags) != 1 || tags[0].Label != "hd" {
		t.Fatalf("CreateTag: %+v", tags)
	}
}
