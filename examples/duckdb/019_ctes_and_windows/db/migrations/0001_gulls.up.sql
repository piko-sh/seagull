CREATE SEQUENCE gull_ids;

CREATE SEQUENCE theft_ids;

CREATE TABLE gulls (
    id        BIGINT  PRIMARY KEY DEFAULT nextval('gull_ids'),
    name      VARCHAR NOT NULL,
    parent_id BIGINT  REFERENCES gulls (id),
    beach     VARCHAR NOT NULL
);

CREATE TABLE thefts (
    id        BIGINT  PRIMARY KEY DEFAULT nextval('theft_ids'),
    gull_id   BIGINT  NOT NULL REFERENCES gulls (id),
    snack     VARCHAR NOT NULL,
    stolen_on VARCHAR NOT NULL
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
