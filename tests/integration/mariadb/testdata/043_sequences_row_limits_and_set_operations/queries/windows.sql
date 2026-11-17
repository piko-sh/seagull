-- sgl.query(name: ShopMedians, command: many)
SELECT id, MEDIAN(items) OVER (PARTITION BY shop) AS median_items,
    PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY items) OVER (PARTITION BY shop) AS continuous,
    PERCENTILE_DISC(0.5) WITHIN GROUP (ORDER BY items DESC) OVER () AS discrete
FROM invoices
ORDER BY id;
