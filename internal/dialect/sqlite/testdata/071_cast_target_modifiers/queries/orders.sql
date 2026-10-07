-- sgl.query(name: OrderConversions, command: many)
SELECT
    CAST(code AS VARCHAR(10)) AS same_code,
    CAST(code AS VARCHAR(20)) AS longer_code,
    CAST(price AS DECIMAL(12, 4)) AS wider_price,
    CAST(placed_at AS TIMESTAMP(3)) AS placed_number
FROM orders
WHERE code = CAST(? AS VARCHAR(10));

-- sgl.query(name: SetLongerCode, command: exec)
UPDATE orders SET code = CAST(? AS VARCHAR(20)) WHERE id = ?;
