-- sgl.query(Elements, many)
SELECT o.id, basket.fish AS fish, pos.name AS position_name, pos.value + 1 AS next_value FROM orders AS o;
