-- sgl.query(name: SelectItems, command: many)
SELECT id, name FROM items;

-- sgl.query(name: InsertItem, command: exec)
INSERT INTO items (id, name) VALUES ($1, $2);

-- sgl.query(name: UpdateItem, command: exec)
UPDATE items SET name = $2 WHERE id = $1;

-- sgl.query(name: DeleteItem, command: exec)
DELETE FROM items WHERE id = $1;
