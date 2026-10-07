-- sgl.query(HandIn, exec)
INSERT INTO lost_property (item, colour, found_at, days_held) VALUES (?, ?, ?, ?);

-- sgl.query(Claim, execrows)
UPDATE lost_property SET claimed = TRUE, owner_phone = ? WHERE id = ?;

-- sgl.query(SearchUnclaimed, many, dynamic: runtime)
SELECT id, item, colour, found_at, days_held
FROM lost_property
WHERE claimed = FALSE
