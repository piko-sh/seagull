CREATE TYPE section AS ENUM ('fish', 'sides', 'puddings');

CREATE TYPE item_status AS ENUM ('available', 'sold_out');

CREATE SEQUENCE menu_item_ids;

CREATE TABLE menu_items (
    id      BIGINT      PRIMARY KEY DEFAULT nextval('menu_item_ids'),
    name    VARCHAR     NOT NULL UNIQUE,
    section section     NOT NULL,
    status  item_status NOT NULL DEFAULT 'available'
);
