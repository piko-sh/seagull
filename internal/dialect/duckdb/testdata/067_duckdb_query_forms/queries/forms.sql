-- sgl.query(name: FromFirst, command: many)
FROM boats SELECT id, name WHERE id = $1;

-- sgl.query(name: HarbourTotals, command: many)
SELECT harbour, sum(kg) AS total FROM landings GROUP BY ALL ORDER BY ALL;

-- sgl.query(name: BiggestLandingPerHarbour, command: many)
SELECT id, harbour, kg FROM landings QUALIFY row_number() OVER (PARTITION BY harbour ORDER BY kg DESC) = 1;

-- sgl.query(name: PricedLandings, command: many)
SELECT l.id, p.price_per_kg FROM landings l ASOF LEFT JOIN prices p ON l.harbour = p.harbour AND l.landed_at >= p.set_at;

-- sgl.query(name: BoatsThatLanded, command: many)
SELECT b.id, b.name FROM boats b SEMI JOIN landings l ON l.boat_id = b.id AND l.harbour = $1;

-- sgl.query(name: AllNames, command: many)
SELECT name FROM boats UNION ALL BY NAME SELECT harbour AS name FROM landings;

-- sgl.query(name: OtherBoats, command: many)
SELECT id FROM boats WHERE id NOT IN ($1, $2);

-- sgl.query(name: BoatCards, command: many)
SELECT {'id': id, 'name': name} AS card FROM boats;

-- sgl.query(name: SamePrice, command: many)
SELECT id FROM landings WHERE price IS NOT DISTINCT FROM $1;
