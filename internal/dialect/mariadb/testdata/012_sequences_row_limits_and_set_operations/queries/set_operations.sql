-- sgl.query(name: ShopsWithDefault, command: many)
VALUES ('none', 0) UNION ALL SELECT shop, items FROM invoices WHERE items > ?;

-- sgl.query(name: CheapestAndDearest, command: many)
(SELECT id, amount FROM invoices ORDER BY amount LIMIT 1) UNION ALL (SELECT id, amount FROM invoices ORDER BY amount DESC LIMIT 1);

-- sgl.query(name: PerItem, command: many)
SELECT id, amount / items AS per_item, CONCAT(shop, ":", id) AS label, STR_TO_DATE(issued, "%Y-%m-%d") AS issued_on
FROM invoices;
