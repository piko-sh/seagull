-- sgl.query(name: InsertItem, command: exec)
INSERT INTO items (id, price, quantity) VALUES (?, ?, ?);

-- sgl.query(name: GetItem, command: one)
SELECT id, price, quantity, total, label FROM items WHERE id = ?;

-- sgl.query(name: ListByMinTotal, command: many)
SELECT id, price, quantity, total FROM items WHERE total >= ? ORDER BY total ASC;
