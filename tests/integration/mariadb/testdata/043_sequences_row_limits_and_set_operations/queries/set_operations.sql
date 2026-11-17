-- sgl.query(name: ShopsWithDefault, command: many)
VALUES ('none', 0) UNION ALL SELECT shop, items FROM invoices WHERE items > ?;

-- sgl.query(name: FewestAndMost, command: many)
(SELECT id, items FROM invoices ORDER BY items, id LIMIT 1) UNION ALL (SELECT id, items FROM invoices ORDER BY items DESC, id LIMIT 1);

-- sgl.query(name: PerItem, command: many)
SELECT id, items DIV (items - 2) AS per_item, CONCAT(shop, ":", id) AS label, STR_TO_DATE(issued, "%Y-%m-%d") AS issued_on
FROM invoices
ORDER BY id;
