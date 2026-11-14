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
  date_trunc('day', p.quote_date) DESC,
  p.id DESC
LIMIT $1 OFFSET $2;

-- sgl.query(name: OrderBySubstring, command: many)
SELECT id FROM quotes ORDER BY substr(description, 1, 10) DESC, id LIMIT $1;

-- sgl.query(name: OrderByCast, command: many)
SELECT id FROM quotes ORDER BY quote_date::date DESC, CAST(price AS text), id;

-- sgl.query(name: OrderByCase, command: many)
SELECT id FROM quotes ORDER BY CASE WHEN category = 'a' THEN 0 WHEN category = 'b' THEN 1 ELSE 2 END, id DESC;

-- sgl.query(name: OrderByCollation, command: many)
SELECT id, description FROM quotes ORDER BY description COLLATE "C" ASC, id;

-- sgl.query(name: OrderByNulls, command: many)
SELECT id FROM quotes ORDER BY deleted_at DESC NULLS LAST, quote_date ASC NULLS FIRST;

-- sgl.query(name: OrderByPosition, command: many)
SELECT id, description FROM quotes ORDER BY 2 DESC, 1;

-- sgl.query(name: OrderByExpression, command: many)
SELECT id FROM quotes ORDER BY price * 2 - 1 DESC, (price + 1), -price, abs(price - $1) LIMIT 10;

-- sgl.query(name: OrderByAlias, command: many)
SELECT id, date_trunc('day', quote_date) AS day FROM quotes ORDER BY day DESC, lower(description);

-- sgl.query(name: WindowOrdering, command: many)
SELECT id, row_number() OVER (PARTITION BY lower(category) ORDER BY date_trunc('day', quote_date) DESC, id) AS position,
  sum(price) OVER (ORDER BY id ROWS BETWEEN 1 PRECEDING AND CURRENT ROW) AS running
FROM quotes ORDER BY position, date_trunc('day', quote_date);

-- sgl.query(name: NamedWindow, command: many)
SELECT id, rank() OVER ranked AS place FROM quotes WHERE price > $1 WINDOW ranked AS (PARTITION BY category ORDER BY price DESC) ORDER BY place;

-- sgl.query(name: AggregateOrdering, command: many)
SELECT category, string_agg(description, ',' ORDER BY lower(description) DESC) AS names
FROM quotes GROUP BY category ORDER BY category;

-- sgl.query(name: GroupedWithHaving, command: many)
SELECT lower(category) AS kind, count(*) AS total, max(date_trunc('day', quote_date)) AS latest
FROM quotes GROUP BY lower(category) HAVING count(*) > $1 AND max(price) < $2 ORDER BY count(*) DESC, lower(category);

-- sgl.query(name: DistinctKinds, command: many)
SELECT DISTINCT lower(category) AS kind FROM quotes ORDER BY lower(category);

-- sgl.query(name: LatestPerKind, command: many)
SELECT DISTINCT ON (category, date_trunc('day', quote_date)) id, category FROM quotes ORDER BY category, date_trunc('day', quote_date), id DESC;

-- sgl.query(name: PageByExpression, command: many)
SELECT id FROM quotes ORDER BY id LIMIT $1 + 1 OFFSET ($2 - 1) * 10;

-- sgl.query(name: FetchFirst, command: many)
SELECT id FROM quotes ORDER BY lower(description) OFFSET $1 ROWS FETCH FIRST $2 ROWS ONLY;

-- sgl.query(name: LockedQuote, command: many)
SELECT id FROM quotes WHERE id = $1 ORDER BY lower(description) LIMIT 1 FOR UPDATE SKIP LOCKED;

-- sgl.query(name: Subqueries, command: many)
SELECT id, (SELECT max(price) FROM quotes inner_quotes WHERE inner_quotes.category = recent.category) AS top
FROM (SELECT id, category, quote_date FROM quotes ORDER BY date_trunc('day', quote_date) DESC LIMIT 100) recent
WHERE id IN (SELECT id FROM quotes ORDER BY lower(description) LIMIT $1)
ORDER BY date_trunc('day', recent.quote_date), id;

-- sgl.query(name: CommonTableExpression, command: many)
WITH recent AS (SELECT id, quote_date FROM quotes ORDER BY date_trunc('day', quote_date) DESC LIMIT $1)
SELECT id FROM recent ORDER BY date_trunc('day', quote_date) ASC, id;

-- sgl.query(name: CompoundOrdered, command: many)
SELECT id, description FROM quotes WHERE category = 'a'
UNION ALL
SELECT id, description FROM quotes WHERE category = 'b'
ORDER BY 2 DESC, 1
LIMIT $1;

-- sgl.query(name: CompoundBranchesOrdered, command: many)
(SELECT id FROM quotes ORDER BY date_trunc('day', quote_date) LIMIT 5)
UNION
(SELECT id FROM quotes ORDER BY lower(description) DESC LIMIT 5)
ORDER BY 1;

-- sgl.query(name: KeywordsInCommentsAndStrings, command: many)
SELECT id /* ORDER BY nonsense LIMIT 5 */, description -- ORDER BY x
FROM quotes
WHERE description <> 'ORDER BY $1 LIMIT ? :name' AND category = $1 AND description <> $tag$ $2 ORDER BY $tag$ -- WHERE $1
ORDER BY /* inline */ lower(description) /* trailing */ DESC -- LIMIT
LIMIT $2 /* end */;

-- sgl.query(name: NamedPlaceholders, command: many)
SELECT id FROM quotes WHERE category = :kind AND price > :minimum ORDER BY abs(price - :minimum) DESC LIMIT :size;
