CREATE TABLE gulls (
    id        UInt64,
    name      String,
    parent_id Nullable(UInt64),
    beach     String
) ENGINE = MergeTree() ORDER BY id;

CREATE TABLE thefts (
    id        UInt64,
    gull_id   UInt64,
    snack     String,
    stolen_on Date
) ENGINE = MergeTree() ORDER BY id;

CREATE VIEW beach_records AS
SELECT
    gulls.beach,
    uniqExact(gulls.id) AS gulls,
    count(thefts.id) AS thefts,
    max(thefts.stolen_on) AS last_theft
FROM gulls
LEFT JOIN thefts ON thefts.gull_id = gulls.id
GROUP BY gulls.beach
SETTINGS join_use_nulls = 1;
