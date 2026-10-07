-- sgl.query(Typed, many)
SELECT 1 AS one, 300 AS wide, -1 AS negative, 1.5 AS fraction, 0x1E AS hex,
       price * 2 AS doubled, id + 1 AS next_id, qty - 1 AS previous, qty / 2 AS halved, intDiv(qty, 2) AS whole, multiply(price, 0.5) AS half,
       lower(basket) AS lowered, coalesce(note, 'none') AS described, ifNull(note, note) AS still_nullable,
       JSONExtractInt(basket, 'portions') AS portions, length(tags) AS tag_count, tags[1] AS first_tag, counts['cod'] AS cod,
       arraySum(weights) AS total_weight, arrayReverse(tags) AS reversed,
       count(DISTINCT orders.id) OVER () AS distinct_orders, rank() OVER (ORDER BY id) AS position,
       CASE WHEN qty > 0 THEN qty ELSE 0 END AS positive
FROM orders;
