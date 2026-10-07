-- sgl.query(name: BookFlags, command: many)
SELECT id, price > 10 AS pricey, price BETWEEN 1 AND 5 AS cheap, price IN (1, 2) AS listed, title LIKE 'A%' AS a_title,
    NOT (price > 10) AS not_pricey, price > 10 OR id = 1 AS either, price IS NULL AS unpriced,
    price IS DISTINCT FROM 1 AS changed
FROM books;
