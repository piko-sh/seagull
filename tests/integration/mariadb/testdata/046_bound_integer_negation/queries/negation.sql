-- sgl.query(NegateInteger, one)
-- ?1 as sgl.param(value)
SELECT -CAST(?1 AS SIGNED) AS value;
