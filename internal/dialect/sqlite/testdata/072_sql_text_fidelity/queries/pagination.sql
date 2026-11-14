-- sgl.query(name: PageByExpression, command: many)
SELECT id FROM quotes ORDER BY date(quote_date) DESC, id LIMIT ? + 1 OFFSET (? - 1) * 10;

-- sgl.query(name: PageByCommaForm, command: many)
SELECT id FROM quotes ORDER BY date(quote_date) DESC, id LIMIT ?, ?;

-- sgl.query(name: PageByCommaFormExpressions, command: many)
SELECT id FROM quotes ORDER BY date(quote_date) DESC, id LIMIT ? * 2, ? + 3;
