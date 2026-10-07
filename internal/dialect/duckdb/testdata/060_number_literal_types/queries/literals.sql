-- sgl.query(name: Literals, command: many)
SELECT .5 AS half, 1.25 AS exact, 1e3 AS approximate, 42 AS small, 3000000000 AS large, 12345678901234567890 AS huge, kg * .5 AS halved
FROM landings
WHERE id = $1;
