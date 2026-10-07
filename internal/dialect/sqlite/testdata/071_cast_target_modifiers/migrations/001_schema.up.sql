-- sgl.column(orders.code, go_type: "html/template.HTML")
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    code VARCHAR(10) NOT NULL,
    price DECIMAL(10, 2) NOT NULL,
    placed_at TEXT NOT NULL
);
