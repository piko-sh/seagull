-- sgl.query(name: ListProductsPaginated, command: many)
-- :page_size as sgl.param
-- :page_offset as sgl.param
SELECT id, name, price FROM products ORDER BY id LIMIT :page_size OFFSET :page_offset;
