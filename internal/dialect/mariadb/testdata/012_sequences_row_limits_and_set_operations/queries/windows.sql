-- sgl.query(name: ShopMedians, command: many)
SELECT id, MEDIAN(amount) OVER (PARTITION BY shop) AS median_amount,
    PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY amount) OVER (PARTITION BY shop) AS continuous,
    PERCENTILE_DISC(0.9) WITHIN GROUP (ORDER BY items DESC) OVER () AS discrete
FROM invoices;
