-- sgl.query(ListProductsPage, many)
-- sgl.sortable(order_by, [name, price])
-- ?1 as sgl.param(min_price)
-- ?2 as sgl.param(page_size, default: 3, max: 5)
-- ?3 as sgl.param(page_offset)
SELECT id, name, price FROM products WHERE price >= ?1 LIMIT ?2 OFFSET ?3;

-- sgl.query(ListProductsInCategories, many)
-- sgl.sortable(order_by, [name, price])
-- ?1 as sgl.param(categories, kind: slice)
-- ?2 as sgl.param(page_size)
SELECT id, name, price FROM products WHERE category IN (?1) LIMIT ?2;

-- sgl.query(FirstProduct, one)
-- sgl.sortable(order_by, [name, price])
SELECT id, name, price FROM products;

-- sgl.query(StreamProducts, stream)
-- sgl.sortable(order_by, [price])
-- ?1 as sgl.param(page_size)
SELECT id, name, price FROM products
LIMIT ?1;
