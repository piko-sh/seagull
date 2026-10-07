CREATE TABLE landings (
    id           BIGINT           NOT NULL AUTO_INCREMENT PRIMARY KEY,
    species      VARCHAR(50)      NOT NULL,
    boxes        SMALLINT         NOT NULL,
    fish         INT              NOT NULL,
    weight_g     BIGINT           NOT NULL,
    length_cm    DOUBLE           NOT NULL,
    on_ice       BOOLEAN          NOT NULL,
    landed_at    DATETIME         NOT NULL,
    tag          VARBINARY(16)    NOT NULL,
    notes        TEXT,
    berth        TINYINT UNSIGNED NOT NULL,
    price_per_kg DECIMAL(6, 2)    NOT NULL
);
