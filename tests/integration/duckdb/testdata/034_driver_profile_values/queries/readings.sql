-- sgl.query(name: AddReading, command: exec)
INSERT INTO readings (id, body, amount, span, note) VALUES ($1, $2, $3, $4, $5);

-- sgl.query(name: GetReading, command: one)
SELECT id, body, amount, span, note, attrs, point FROM readings WHERE id = $1;

-- sgl.query(name: ReadingsOver, command: many)
SELECT id, amount FROM readings WHERE amount > $1 ORDER BY id;
