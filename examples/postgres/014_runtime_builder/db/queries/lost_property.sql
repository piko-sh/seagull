-- sgl.query(HandIn, exec)
INSERT INTO lost_property (item, colour, found_at, days_held) VALUES ($1, $2, $3, $4);

-- sgl.query(Claim, execrows)
UPDATE lost_property SET claimed = true, owner_phone = $1 WHERE id = $2;

-- sgl.query(SearchUnclaimed, many, dynamic: runtime)
SELECT id, item, colour, found_at, days_held
FROM lost_property
WHERE NOT claimed
