-- sgl.query(name: ListProductsSorted, command: many)
-- sgl.sortable(order_by, columns: [name, price])
SELECT id, name, price, category FROM products

-- sgl.query(name: ListProductsPaginated, command: many)
-- ?1 as sgl.param(page_size, default: 5, max: 20)
-- ?2 as sgl.param(page_offset)
SELECT id, name, price, category FROM products ORDER BY id ASC LIMIT ?1 OFFSET ?2;
