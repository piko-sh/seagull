-- sgl.query(Compute, many)
SELECT Doubled(value) AS twice, doubled(value) AS text_form FROM readings;

-- sgl.query(Shouted, many)
SELECT DOUBLED(value) AS shouted FROM readings;
