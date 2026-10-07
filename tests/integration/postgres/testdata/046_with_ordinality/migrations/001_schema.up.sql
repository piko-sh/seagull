CREATE TABLE playlists (
    id integer PRIMARY KEY,
    tracks text[] NOT NULL,
    ratings integer[]
);
