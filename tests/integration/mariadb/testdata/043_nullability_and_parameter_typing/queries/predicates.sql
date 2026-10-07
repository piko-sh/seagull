-- sgl.query(name: BookFlags, command: many)
SELECT id, price > 10 AS pricey, price BETWEEN 1 AND 10 AS cheap, price IN (9, 12) AS listed, NOT (price > 10) AS not_pricey,
    price > 10 OR id = 1 AS either, price IS NULL AS unpriced, price <=> 12 AS same
FROM books
ORDER BY id;
