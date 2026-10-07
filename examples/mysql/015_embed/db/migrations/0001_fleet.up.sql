CREATE TABLE harbours (
    id     BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name   VARCHAR(50) NOT NULL,
    county VARCHAR(50) NOT NULL
);

CREATE TABLE skippers (
    id      BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name    VARCHAR(50) NOT NULL,
    licence VARCHAR(20) NOT NULL
);

CREATE TABLE boats (
    id         BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(50) NOT NULL,
    harbour_id BIGINT      NOT NULL,
    skipper_id BIGINT,
    FOREIGN KEY (harbour_id) REFERENCES harbours (id),
    FOREIGN KEY (skipper_id) REFERENCES skippers (id)
);
