-- +goose Up
-- Verified-language knowledge base: the real audio/subtitle languages of a
-- downloaded release (from the Plex stream check), keyed by release name. Drives
-- group-tendency learning in selection, and is shaped for a future shared/export
-- service (release_name + group + langs + source).
CREATE TABLE release_lang (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    release_name  TEXT NOT NULL,
    release_group TEXT NOT NULL DEFAULT '',
    audio_langs   TEXT NOT NULL DEFAULT '', -- csv of 2-letter codes (de,en,ja)
    sub_langs     TEXT NOT NULL DEFAULT '', -- csv of 2-letter codes
    source        TEXT NOT NULL DEFAULT 'plex',
    detected_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(release_name)
);
CREATE INDEX idx_release_lang_group ON release_lang(release_group);

-- +goose Down
DROP TABLE release_lang;
