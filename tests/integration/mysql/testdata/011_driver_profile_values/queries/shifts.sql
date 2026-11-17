-- sgl.query(name: AddShift, command: exec)
INSERT INTO shifts (starts_at, season, flags, ends_at) VALUES (?, ?, ?, ?);

-- sgl.query(name: GetShift, command: one)
SELECT id, starts_at, season, flags, ends_at FROM shifts WHERE id = ?;

-- sgl.query(name: ShiftsInSeason, command: many)
SELECT id FROM shifts WHERE season = ? ORDER BY id;
