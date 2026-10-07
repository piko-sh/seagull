-- sgl.query(name: ListOrders, command: many)
SELECT * FROM orders;

-- sgl.query(name: QuantityPerPart, command: many)
SELECT id, quantity / parts AS per_part, quantity // parts AS whole_parts
FROM orders;

-- sgl.query(name: DaysToShip, command: many)
SELECT id, shipped_on - ordered_on AS days, ordered_on + 3 AS promised_on
FROM orders;

-- sgl.query(name: HalfDays, command: many)
SELECT moment FROM generate_series('2024-01-01'::TIMESTAMP, '2024-01-02'::TIMESTAMP, '12 hours'::INTERVAL) AS moment ORDER BY moment;
