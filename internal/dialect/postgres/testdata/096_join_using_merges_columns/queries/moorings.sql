-- sgl.query(name: MooringsUsing, command: many)
SELECT *
FROM harbours
JOIN moorings USING (harbour_id);

-- sgl.query(name: MooringsNatural, command: many)
SELECT *
FROM harbours
NATURAL JOIN moorings;

-- sgl.query(name: MooringsLeftUsing, command: many)
SELECT *
FROM harbours
LEFT JOIN moorings USING (harbour_id);

-- sgl.query(name: MooringHarbourIDs, command: many)
SELECT harbour_id, berth
FROM harbours
JOIN moorings USING (harbour_id);

-- sgl.query(name: BusiestHarbours, command: many)
SELECT *
FROM (SELECT harbour_id, count(*) AS berths FROM moorings GROUP BY harbour_id) busy
JOIN harbours USING (harbour_id);
