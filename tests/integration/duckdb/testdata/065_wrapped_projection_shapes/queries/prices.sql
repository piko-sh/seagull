-- sgl.query(OrderedAmounts, many)
SELECT id, amount FROM prices ORDER BY prices.amount;

-- sgl.query(DistinctWaits, many)
SELECT w.wait FROM (SELECT DISTINCT wait FROM prices) AS w ORDER BY w.wait;

-- sgl.query(DistinctAmounts, many)
SELECT DISTINCT amount FROM prices;

-- sgl.query(UnionAmounts, many)
SELECT u.amount FROM (SELECT amount FROM prices UNION SELECT amount FROM prices) AS u ORDER BY u.amount;

-- sgl.query(AliasOrderedAmounts, many)
SELECT id, amount FROM prices ORDER BY amount;

-- sgl.query(OrdinalOrderedAmounts, many)
SELECT amount, id FROM prices ORDER BY 1, 2;

-- sgl.query(DistinctWaitsDirect, many)
SELECT DISTINCT wait FROM prices ORDER BY wait;

-- sgl.query(UnionAmountsDirect, many)
SELECT amount FROM prices UNION SELECT amount FROM prices ORDER BY 1;
