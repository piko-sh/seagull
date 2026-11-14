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
  date(p.quote_date) DESC,
  p.id DESC
LIMIT ? OFFSET ?;

-- sgl.query(name: OrderBySubstring, command: many)
SELECT id FROM quotes ORDER BY substr(description, 1, 10) DESC, id LIMIT ?;

-- sgl.query(name: OrderByCase, command: many)
SELECT id FROM quotes ORDER BY CASE WHEN category = 'a' THEN 0 WHEN category = 'b' THEN 1 ELSE 2 END, id DESC;

-- sgl.query(name: OrderByCollation, command: many)
SELECT id, description FROM quotes ORDER BY description COLLATE utf8mb4_bin ASC, id;

-- sgl.query(name: OrderByField, command: many)
SELECT id FROM quotes ORDER BY FIELD(category, 'b', 'a', 'c'), IF(deleted_at IS NULL, 0, 1), id;

-- sgl.query(name: OrderByPosition, command: many)
SELECT id, description FROM quotes ORDER BY 2 DESC, 1;

-- sgl.query(name: OrderByExpression, command: many)
SELECT id FROM quotes ORDER BY price * 2 - 1 DESC, (price + 1), -price, abs(price - ?) LIMIT 10;

-- sgl.query(name: OrderByAlias, command: many)
SELECT id, date(quote_date) AS day FROM quotes ORDER BY day DESC, lower(description);

-- sgl.query(name: WindowOrdering, command: many)
SELECT id, row_number() OVER (PARTITION BY lower(category) ORDER BY date(quote_date) DESC, id) AS position,
  sum(price) OVER (ORDER BY id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) AS running
FROM quotes ORDER BY position, date(quote_date);

-- sgl.query(name: NamedWindow, command: many)
SELECT id, rank() OVER ranked AS place FROM quotes WHERE price > ? WINDOW ranked AS (PARTITION BY category ORDER BY price DESC) ORDER BY place;

-- sgl.query(name: AggregateOrdering, command: many)
SELECT category, GROUP_CONCAT(description ORDER BY lower(description) DESC SEPARATOR ',') AS names
FROM quotes GROUP BY category ORDER BY category;

-- sgl.query(name: GroupedWithHaving, command: many)
SELECT lower(category) AS kind, count(*) AS total, max(date(quote_date)) AS latest
FROM quotes GROUP BY lower(category) HAVING count(*) > ? AND max(price) < ? ORDER BY count(*) DESC, lower(category);

-- sgl.query(name: Subtotals, command: many)
SELECT category, count(*) AS total FROM quotes GROUP BY category WITH ROLLUP ORDER BY category;

-- sgl.query(name: DistinctKinds, command: many)
SELECT DISTINCT lower(category) AS kind FROM quotes ORDER BY lower(category);

-- sgl.query(name: PageByCommaForm, command: many)
SELECT id FROM quotes ORDER BY lower(description) LIMIT ?, ?;

-- sgl.query(name: LockedQuote, command: many)
SELECT id FROM quotes WHERE id = ? ORDER BY lower(description) LIMIT 1 FOR UPDATE SKIP LOCKED;

-- sgl.query(name: Subqueries, command: many)
SELECT id, (SELECT max(price) FROM quotes inner_quotes WHERE inner_quotes.category = recent.category) AS top
FROM (SELECT id, category, quote_date FROM quotes ORDER BY date(quote_date) DESC LIMIT 100) recent
WHERE id IN (SELECT id FROM (SELECT id FROM quotes ORDER BY lower(description) LIMIT 10) limited)
ORDER BY date(recent.quote_date), id;

-- sgl.query(name: CommonTableExpression, command: many)
WITH recent AS (SELECT id, quote_date FROM quotes ORDER BY date(quote_date) DESC LIMIT 10)
SELECT id FROM recent ORDER BY date(quote_date) ASC, id;

-- sgl.query(name: CompoundOrdered, command: many)
SELECT id, description FROM quotes WHERE category = 'a'
UNION ALL
SELECT id, description FROM quotes WHERE category = 'b'
ORDER BY 2 DESC, 1
LIMIT ?;

-- sgl.query(name: CompoundBranchesOrdered, command: many)
(SELECT id FROM quotes ORDER BY date(quote_date) LIMIT 5)
UNION
(SELECT id FROM quotes ORDER BY lower(description) DESC LIMIT 5)
ORDER BY 1;

-- sgl.query(name: KeywordsInCommentsAndStrings, command: many)
SELECT id /* ORDER BY nonsense LIMIT 5 */, description -- ORDER BY x
FROM quotes
WHERE description <> 'ORDER BY ? LIMIT \'?\'' AND category = ? # WHERE ?
ORDER BY /* inline */ lower(description) /* trailing */ DESC -- LIMIT
LIMIT ? /* end */;

-- sgl.query(name: QuotedIdentifiers, command: many)
SELECT `id`, `description` AS `order by ?` FROM `quotes` ORDER BY `order by ?` DESC, lower(`category`);
