-- sgl.query(name: ListProductsPaginated, command: many)
-- ?1 as sgl.param(page_size)
-- ?2 as sgl.param(page_offset)
SELECT id, name, price FROM products ORDER BY id LIMIT ?1 OFFSET ?2;

-- sgl.query(name: ListProductsByMinPrice, command: many)
-- ?1 as sgl.param(minimum_price)
-- ?2 as sgl.param(page_size)
-- ?3 as sgl.param(page_offset)
SELECT id, name, price FROM products WHERE price >= ? ORDER BY price LIMIT ? OFFSET ?;
