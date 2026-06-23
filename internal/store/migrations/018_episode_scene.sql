-- +goose Up
-- Authoritative scene/broadcast numbering for an episode, set from TheTVDB when a
-- key is configured. Lets the anime search map TMDB's flat numbering to the
-- season/episode that release groups actually use (0 = not set → fall back to the
-- air-date-gap heuristic). absolute_number already exists.
ALTER TABLE episode ADD COLUMN scene_season INTEGER NOT NULL DEFAULT 0;
ALTER TABLE episode ADD COLUMN scene_episode INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE episode DROP COLUMN scene_season;
ALTER TABLE episode DROP COLUMN scene_episode;
