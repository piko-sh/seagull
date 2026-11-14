-- sgl.query(name: ListQuotes, command: many)
SELECT
  p.id,
  p.description,
  p.quote_date,
  c.company_name AS customer_name
FROM
  quotes p
JOIN
  customers c ON p.customer_id = c.id
WHERE
  p.deleted_at IS NULL
ORDER BY
  toDate(p.quote_date) DESC,
  p.id DESC
LIMIT {limit:UInt64} OFFSET {offset:UInt64};

-- sgl.query(name: OrderBySubstring, command: many)
SELECT id FROM quotes ORDER BY substring(description, 1, 10) DESC, id LIMIT {limit:UInt64};

-- sgl.query(name: OrderByCase, command: many)
SELECT id FROM quotes ORDER BY CASE WHEN category = 'a' THEN 0 WHEN category = 'b' THEN 1 ELSE 2 END, id DESC;

-- sgl.query(name: OrderByCollation, command: many)
SELECT id, description FROM quotes ORDER BY description ASC COLLATE 'en', lower(category) DESC NULLS LAST COLLATE 'de', id;

-- sgl.query(name: OrderByNulls, command: many)
SELECT id FROM quotes ORDER BY deleted_at DESC NULLS LAST, quote_date ASC NULLS FIRST;

-- sgl.query(name: OrderByPosition, command: many)
SELECT id, description FROM quotes ORDER BY 2 DESC, 1;

-- sgl.query(name: OrderByExpression, command: many)
SELECT id FROM quotes ORDER BY price * 2 - 1 DESC, (price + 1), -price, abs(price - {target:Int32}) LIMIT 10;

-- sgl.query(name: OrderByAlias, command: many)
SELECT id, toDate(quote_date) AS day FROM quotes ORDER BY day DESC, lower(description);

-- sgl.query(name: OrderWithFill, command: many)
SELECT toDate(quote_date) AS day, count() AS total FROM quotes GROUP BY day ORDER BY day WITH FILL STEP 1;

-- sgl.query(name: WindowOrdering, command: many)
SELECT id, row_number() OVER (PARTITION BY lower(category) ORDER BY toDate(quote_date) DESC, id) AS position
FROM quotes ORDER BY position, toDate(quote_date);

-- sgl.query(name: LimitBy, command: many)
SELECT id, category FROM quotes ORDER BY category, toDate(quote_date) DESC LIMIT 2 BY category LIMIT {limit:UInt64};

-- sgl.query(name: GroupedWithHaving, command: many)
SELECT lower(category) AS kind, count() AS total, max(toDate(quote_date)) AS latest
FROM quotes GROUP BY lower(category) HAVING count() > {minimum:UInt64} ORDER BY count() DESC, lower(category);

-- sgl.query(name: Totals, command: many)
SELECT category, count() AS total FROM quotes GROUP BY category WITH TOTALS ORDER BY category;

-- sgl.query(name: DistinctKinds, command: many)
SELECT DISTINCT lower(category) AS kind FROM quotes ORDER BY lower(category);

-- sgl.query(name: PageByCommaForm, command: many)
SELECT id FROM quotes ORDER BY lower(description) LIMIT 5, 10;

-- sgl.query(name: PageByParameterCommaForm, command: many)
SELECT id FROM quotes ORDER BY lower(description) LIMIT {o:UInt64}, {l:UInt64};

-- sgl.query(name: Settings, command: many)
SELECT id FROM quotes ORDER BY toDate(quote_date) DESC LIMIT 5 SETTINGS max_threads = 1;

-- sgl.query(name: Subqueries, command: many)
SELECT id
FROM (SELECT id, quote_date FROM quotes ORDER BY toDate(quote_date) DESC LIMIT 100) recent
WHERE id IN (SELECT id FROM quotes ORDER BY lower(description) LIMIT 10)
ORDER BY toDate(recent.quote_date), id;

-- sgl.query(name: CommonTableExpression, command: many)
WITH recent AS (SELECT id, quote_date FROM quotes ORDER BY toDate(quote_date) DESC LIMIT 10)
SELECT id FROM recent ORDER BY toDate(quote_date) ASC, id;

-- sgl.query(name: CompoundOrdered, command: many)
SELECT id, description FROM quotes WHERE category = 'a'
UNION ALL
SELECT id, description FROM quotes WHERE category = 'b'
ORDER BY 2 DESC, 1
LIMIT 5;

-- sgl.query(name: KeywordsInCommentsAndStrings, command: many)
SELECT id /* ORDER BY nonsense LIMIT 5 */, description -- ORDER BY x
FROM quotes
WHERE description <> 'ORDER BY {a:UInt8} LIMIT ?' AND category = {category:String} -- WHERE ?
ORDER BY /* inline */ lower(description) /* trailing */ DESC -- LIMIT
LIMIT 5 /* end */;
