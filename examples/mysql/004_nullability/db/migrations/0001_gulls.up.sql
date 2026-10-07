CREATE TABLE gulls (
    id       BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    ring     VARCHAR(10) NOT NULL UNIQUE,
    nickname VARCHAR(50)
);

CREATE TABLE sightings (
    id      BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    gull_id BIGINT       NOT NULL,
    seen_on DATE         NOT NULL,
    place   VARCHAR(100) NOT NULL,
    FOREIGN KEY (gull_id) REFERENCES gulls (id)
);
