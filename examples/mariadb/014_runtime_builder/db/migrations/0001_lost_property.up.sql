CREATE TABLE lost_property (
    id          BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    item        VARCHAR(50) NOT NULL,
    colour      VARCHAR(20) NOT NULL,
    found_at    VARCHAR(50) NOT NULL,
    days_held   INT         NOT NULL,
    claimed     BOOLEAN     NOT NULL DEFAULT FALSE,
    owner_phone VARCHAR(20)
);
