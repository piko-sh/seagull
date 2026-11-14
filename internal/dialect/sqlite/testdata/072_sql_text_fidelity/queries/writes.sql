-- sgl.query(name: RenameQuote, command: one)
UPDATE quotes SET description = upper(?), price = price + 1 WHERE id = ? RETURNING id, lower(description) AS lowered, date(quote_date) AS day;

-- sgl.query(name: UpsertQuote, command: exec)
INSERT INTO quotes (id, description, category, price, quote_date, customer_id) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET description = excluded.description || ' (' || date('now') || ')', price = max(quotes.price, excluded.price)
WHERE quotes.price < excluded.price;

-- sgl.query(name: CopyLatestQuote, command: exec)
INSERT INTO quotes (description, category, price, quote_date, customer_id)
SELECT description, category, price, quote_date, customer_id FROM quotes WHERE id = ? ORDER BY date(quote_date) DESC LIMIT 1;

-- sgl.query(name: DeleteOldestQuotes, command: many)
DELETE FROM quotes WHERE id IN (SELECT id FROM quotes ORDER BY date(quote_date) LIMIT ?) RETURNING id, upper(category) AS kind;
