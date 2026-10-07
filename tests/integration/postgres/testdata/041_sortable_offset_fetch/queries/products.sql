-- sgl.query(ListProductsPage, many)
-- sgl.sortable(order_by, [name, price])
-- $1 as sgl.param(min_price)
-- $2 as sgl.param(page_size, default: 10, max: 100)
-- $3 as sgl.param(page_offset)
SELECT id, name, price FROM products WHERE price >= $1 LIMIT $2 OFFSET $3;

-- sgl.query(FetchProductsPage, many)
-- sgl.sortable(order_by, [name, price])
-- $1 as sgl.param(page_offset)
-- $2 as sgl.param(page_size)
SELECT id, name, price FROM products OFFSET $1 ROWS FETCH NEXT $2 ROWS ONLY;
