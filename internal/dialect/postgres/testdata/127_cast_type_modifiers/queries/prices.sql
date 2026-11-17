-- sgl.query(name: CastResults, command: one)
SELECT amount::numeric(12, 4) AS widened,
       CAST(sku AS varchar(8)) AS short_sku,
       seen::timestamp(0) without time zone AS local_seen,
       opened::time(3) with time zone AS zoned_opened,
       CAST(seen AS timestamp(6) with time zone) AS precise_seen,
       hundreds::numeric(7, -3) AS thousands
FROM prices
WHERE id = $1;

-- sgl.query(name: CastParameters, command: many)
SELECT id
FROM prices
WHERE amount = $1::numeric(10, 2)
  AND sku = CAST($2 AS varchar(40))
  AND seen > $3::timestamp(3) with time zone
  AND opened < CAST($4 AS time(0) without time zone);

-- sgl.query(name: InsertPrice, command: exec)
INSERT INTO prices (id, amount, hundreds, sku, seen, opened)
VALUES ($1, $2::numeric(10, 2), $3::numeric(5, -2), $4, $5, $6);
