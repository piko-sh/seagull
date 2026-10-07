CREATE TABLE menu_items (
    id      BIGINT                              NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name    VARCHAR(50)                         NOT NULL UNIQUE,
    section ENUM('fish', 'sides', 'puddings')   NOT NULL,
    status  ENUM('available', 'sold_out')       NOT NULL DEFAULT 'available'
);
