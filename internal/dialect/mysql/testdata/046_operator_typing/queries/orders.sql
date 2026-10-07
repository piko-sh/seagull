-- sgl.query(OrderRatios, one)
SELECT quantity / 3 AS per_third,
       unit_price / quantity AS unit_share,
       weight / 2 AS half_weight,
       quantity DIV 2 AS halves,
       flags & 4 AS masked,
       flags << 1 AS shifted
FROM orders
WHERE id = ?;

-- sgl.query(OrderLabels, many)
SELECT CONCAT_WS(',', note, label) AS joined,
       CONCAT_WS(separator_text, label) AS by_separator
FROM orders;

-- sgl.query(OrderDetails, many)
SELECT details -> '$.colour' AS colour_json,
       details ->> '$.colour' AS colour
FROM orders;

-- sgl.query(BusyOrders, many)
SELECT id, quantity > 1 || flags > 0 AS busy
FROM orders;
