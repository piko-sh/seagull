-- sgl.query(name: RenameNewestQuotes, command: exec)
UPDATE quotes SET description = upper(?), price = price + 1 WHERE category = ? ORDER BY date(quote_date) DESC, id LIMIT 5;

-- sgl.query(name: DeleteOldestQuotes, command: exec)
DELETE FROM quotes WHERE category = ? ORDER BY date(quote_date) LIMIT 10;

-- sgl.query(name: UpsertQuote, command: exec)
INSERT INTO quotes (id, description, category, price, quote_date, customer_id) VALUES (?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE description = CONCAT('(', VALUES(description), ')'), price = GREATEST(price, VALUES(price));

-- sgl.query(name: CopyLatestQuote, command: exec)
INSERT INTO quotes (description, category, price, quote_date, customer_id)
SELECT description, category, price, quote_date, customer_id FROM quotes WHERE id = ? ORDER BY date(quote_date) DESC LIMIT 1;
