-- sgl.column(menu_items.price_pence, go_type: "seagull.sh/seagull/examples/sqlite/009_overrides/internal/menu.Pence")
CREATE TABLE menu_items (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    section     TEXT    NOT NULL,
    status      TEXT    NOT NULL CHECK (status IN ('available', 'sold_out')),
    price_pence INTEGER NOT NULL,
    nutrition   TEXT    NOT NULL
);
