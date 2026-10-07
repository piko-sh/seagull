CREATE TABLE gulls (
    id        INTEGER PRIMARY KEY,
    name      TEXT    NOT NULL,
    parent_id INTEGER REFERENCES gulls (id),
    beach     TEXT    NOT NULL
);

CREATE TABLE thefts (
    id        INTEGER PRIMARY KEY,
    gull_id   INTEGER NOT NULL REFERENCES gulls (id),
    snack     TEXT    NOT NULL,
    stolen_on TEXT    NOT NULL
);

CREATE VIEW beach_records AS
SELECT
    gulls.beach,
    COUNT(DISTINCT gulls.id) AS gulls,
    COUNT(thefts.id) AS thefts,
    MAX(thefts.stolen_on) AS last_theft
FROM gulls
LEFT JOIN thefts ON thefts.gull_id = gulls.id
GROUP BY gulls.beach;
