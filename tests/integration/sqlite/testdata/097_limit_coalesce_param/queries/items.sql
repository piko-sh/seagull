-- sgl.query(name: ListItems, command: many)
-- ?1 as sgl.param(page_size)
SELECT id, name FROM items ORDER BY id LIMIT COALESCE(CAST(?1 AS INTEGER), 10);
