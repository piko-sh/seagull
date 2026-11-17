-- sgl.query(name: SortableQuotes, command: many)
-- sgl.sortable(order_by, [id, price, quote_date])
SELECT id, price, quote_date FROM quotes WHERE deleted_at IS NULL ORDER BY toDate(quote_date) DESC, id LIMIT {limit:UInt64};

-- sgl.query(name: SearchQuotes, command: many, dynamic: runtime)
SELECT id, price, quote_date
FROM quotes
ORDER BY toDate(quote_date) DESC, id
LIMIT 10 SETTINGS max_threads = 1;

-- sgl.query(name: SearchTags, command: many, dynamic: runtime)
SELECT id, arrayJoin(tags) AS tag FROM quotes ORDER BY id;

-- sgl.query(name: SearchByTag, command: many, dynamic: runtime)
SELECT id FROM quotes ORDER BY arrayJoin(tags) LIMIT 10;
