-- sgl.query(name: RenameQuote, command: one)
UPDATE quotes SET description = upper($1), price = price + 1 WHERE id = $2 RETURNING id, lower(description) AS lowered, date_trunc('day', quote_date) AS day;

-- sgl.query(name: UpsertQuote, command: exec)
INSERT INTO quotes (id, description, category, price, tags, quote_date, customer_id) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE SET description = excluded.description || ' (' || CAST(now() AS VARCHAR) || ')', price = greatest(quotes.price, excluded.price)
WHERE quotes.price < excluded.price;

-- sgl.query(name: CopyLatestQuote, command: exec)
INSERT INTO quotes (id, description, category, price, tags, quote_date, customer_id)
SELECT id + 1000, description, category, price, tags, quote_date, customer_id FROM quotes WHERE id = $1 ORDER BY date_trunc('day', quote_date) DESC LIMIT 1;

-- sgl.query(name: DeleteOldestQuotes, command: many)
DELETE FROM quotes WHERE id IN (SELECT id FROM quotes ORDER BY date_trunc('day', quote_date) LIMIT $1) RETURNING id, upper(category) AS kind;
