-- sgl.query(name: HeavyLandings, command: many)
SELECT b.name, l.kg FROM (boats b JOIN landings l ON l.boat_id = b.id) WHERE l.kg > $1;

-- sgl.query(name: BoatsWithLandings, command: many)
SELECT b.name AS boat, h.name AS harbour, l.kg
FROM boats b
LEFT JOIN (landings l JOIN harbours h ON h.id = l.harbour_id) ON l.boat_id = b.id;
