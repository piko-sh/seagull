-- sgl.query(AddItem, exec)
INSERT INTO items (name, qty) VALUES (?1, ?2);

-- sgl.query(GetItem, one)
SELECT id, name, qty FROM items WHERE name = ?1;

-- sgl.query(ListItemsSorted, many)
-- sgl.sortable(order_by, [name, qty])
SELECT id, name, qty FROM items;
