-- +goose Up
-- Alternative + original titles (from TMDB) for cross-language matching, so a
-- German-titled release of an English-catalogued movie (e.g. "Harry Potter und
-- der Gefangene von Askaban") is recognized as tracked. Newline-separated.
ALTER TABLE movie ADD COLUMN alt_titles TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE movie DROP COLUMN alt_titles;
