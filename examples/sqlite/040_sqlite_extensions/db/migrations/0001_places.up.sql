CREATE TABLE places (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL,
    tags        TEXT    NOT NULL DEFAULT '[]',
    west        REAL    NOT NULL,
    east        REAL    NOT NULL,
    south       REAL    NOT NULL,
    north       REAL    NOT NULL,
    tag_count   INTEGER NOT NULL GENERATED ALWAYS AS (json_array_length(tags)) VIRTUAL
);

CREATE VIRTUAL TABLE places_search USING fts5(name, description, content=places, content_rowid=id);

CREATE VIRTUAL TABLE places_area USING rtree(id, min_x, max_x, min_y, max_y);

CREATE TRIGGER places_indexed AFTER INSERT ON places BEGIN
    INSERT INTO places_search (rowid, name, description) VALUES (new.id, new.name, new.description);
    INSERT INTO places_area (id, min_x, max_x, min_y, max_y) VALUES (new.id, new.west, new.east, new.south, new.north);
END;
