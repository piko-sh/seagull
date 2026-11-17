-- sgl.query(name: Ratios, command: many)
SELECT id, spend / visits AS per_visit, visits % visits AS remainder, visits DIV visits AS whole, spend / 2 AS half
FROM people
ORDER BY id;

-- sgl.query(name: Dates, command: many)
SELECT id, STR_TO_DATE(born, '%Y-%m-%d') AS parsed, CAST(born AS DATE) AS cast_date, CAST(signed_up AS DATE) AS signed_up_day
FROM people
ORDER BY id;

-- sgl.query(name: SearchPipes, command: many)
SELECT id FROM people WHERE first_name LIKE '%' || ? || '%' ORDER BY id;
