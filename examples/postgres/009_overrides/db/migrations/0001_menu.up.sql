-- sgl.column(menu_items.price, go_type: "seagull.sh/seagull/examples/postgres/009_overrides/internal/menu.Price")
CREATE TABLE menu_items (
    id        BIGINT        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name      TEXT          NOT NULL,
    section   TEXT          NOT NULL,
    status    TEXT          NOT NULL CHECK (status IN ('available', 'sold_out')),
    price     NUMERIC(5, 2) NOT NULL,
    nutrition JSONB         NOT NULL
);
