-- sgl.query(name: PriceConversions, command: many)
SELECT
    CAST(amount AS DECIMAL(10, 2)) AS same_amount,
    CAST(amount AS DECIMAL(12, 4)) AS wider_amount,
    CONVERT(label, CHAR(20)) AS short_label,
    CAST(listed_at AS DATETIME(6)) AS listed_precisely,
    JSON_VALUE(details, '$.discount' RETURNING DECIMAL(5, 2)) AS discount
FROM prices
WHERE amount > CAST(? AS DECIMAL(10, 2));

-- sgl.query(name: SetAmount, command: exec)
UPDATE prices SET amount = CAST(? AS DECIMAL(10, 2)) WHERE id = ?;

-- sgl.query(name: SetWiderAmount, command: exec)
UPDATE prices SET amount = CAST(? AS DECIMAL(12, 4)) WHERE id = ?;
