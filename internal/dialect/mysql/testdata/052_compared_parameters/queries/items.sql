-- sgl.query(name: MatchItems, command: many)
SELECT
    id,
    id + 1 = ? AS matched,
    CASE WHEN stock > ? THEN 'many' ELSE 'few' END AS level
FROM items
WHERE price * 2 > ?
  AND coalesce(stock, 0) < ?
  AND name = ?;

-- sgl.query(name: CountBusyNames, command: many)
SELECT name, count(*) AS total
FROM items
GROUP BY name
HAVING count(*) + 1 >= ?;
