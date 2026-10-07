-- sgl.query(AddShift, one)
INSERT INTO shifts (keeper, day) VALUES (?, ?) RETURNING id;

-- sgl.query(CoverShift, execrows)
UPDATE shifts SET covered = 1 WHERE id = ?;

-- sgl.query(ListShifts, many)
SELECT id, keeper, day, covered FROM shifts ORDER BY id;
