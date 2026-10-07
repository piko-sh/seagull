CREATE TABLE gulls (
    id        BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name      VARCHAR(50) NOT NULL,
    parent_id BIGINT,
    beach     VARCHAR(50) NOT NULL,
    FOREIGN KEY (parent_id) REFERENCES gulls (id)
);

CREATE TABLE thefts (
    id        BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    gull_id   BIGINT      NOT NULL,
    snack     VARCHAR(50) NOT NULL,
    stolen_on DATE        NOT NULL,
    FOREIGN KEY (gull_id) REFERENCES gulls (id)
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
