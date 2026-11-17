-- sgl.query(name: ScaledPrice, command: one)
SELECT scale_price($1, $2) AS price;

-- sgl.query(name: ScaledPriceByName, command: one)
SELECT scale_price(factor => $1, amount => $2) AS price;

-- sgl.query(name: ScaledPriceByOldName, command: one)
SELECT scale_price(factor := $1, amount := $2) AS price;

-- sgl.query(name: TidyCatches, command: exec)
CALL tidy_catches($1, $2);
