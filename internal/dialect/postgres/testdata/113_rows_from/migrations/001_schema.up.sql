CREATE TABLE playlists (
    id integer PRIMARY KEY,
    tracks text[] NOT NULL,
    ratings integer[],
    settings jsonb NOT NULL
);
