-- sgl.query(name: MatchItems, command: many)
SELECT
    id,
    id + 1 = ? AS matched,
    CASE WHEN stock > :threshold THEN 'many' ELSE 'few' END AS level
FROM items
WHERE price * 2 > ?3
  AND coalesce(stock, 0) < @ceiling
  AND name = $name
  AND stock <> :threshold;

-- sgl.query(name: CountBusyNames, command: many)
SELECT name, count(*) AS total
FROM items
GROUP BY name
HAVING count(*) + 1 >= ?;
