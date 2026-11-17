-- sgl.query(name: InsertWidths, command: exec)
INSERT INTO widths (id, tiny, small, medium, regular, big, utiny, usmall, umedium, uregular, ubig)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- sgl.query(name: GetWidths, command: one)
SELECT id, tiny, small, medium, regular, big, utiny, usmall, umedium, uregular, ubig
FROM widths
WHERE id = ?;
