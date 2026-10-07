-- sgl.query(name: AddShift, command: one)
INSERT INTO shifts (starts_at, length, ends_at) VALUES ($1, $2, $3) RETURNING id;

-- sgl.query(name: GetShift, command: one)
SELECT id, starts_at, length, ends_at FROM shifts WHERE id = $1;

-- sgl.query(name: ShiftsLongerThan, command: many)
SELECT id, length FROM shifts WHERE length > $1 ORDER BY id;
