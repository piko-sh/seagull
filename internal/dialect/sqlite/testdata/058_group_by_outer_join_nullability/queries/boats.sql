-- sgl.query(name: CrewByBoat, command: many)
SELECT b.id, b.crew, count(h.id) AS harbours
FROM harbours h
LEFT JOIN boats b ON b.harbour_id = h.id
GROUP BY b.id;
