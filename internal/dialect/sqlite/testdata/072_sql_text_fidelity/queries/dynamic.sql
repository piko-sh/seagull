-- sgl.query(name: SortableQuotes, command: many)
-- sgl.sortable(order_by, [id, price, quote_date])
SELECT id, price, quote_date FROM quotes WHERE deleted_at IS NULL ORDER BY date(quote_date) DESC, id LIMIT ?;

-- sgl.query(name: SearchQuotes, command: many, dynamic: runtime)
SELECT id, price, quote_date
FROM quotes
WHERE deleted_at IS NULL
ORDER BY date(quote_date) DESC, id
LIMIT 10;
