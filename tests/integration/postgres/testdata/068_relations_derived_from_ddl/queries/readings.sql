-- sgl.query(name: AddReading, command: exec)
INSERT INTO readings (id, taken_on, value, note) VALUES ($1, $2, $3, $4);

-- sgl.query(name: ListReadings2026, command: many)
SELECT id, taken_on, value, note FROM readings_2026 ORDER BY id;

-- sgl.query(name: ListLowReadings, command: many)
SELECT * FROM readings_2027_low ORDER BY id;

-- sgl.query(name: ListAttachedReadings, command: many)
SELECT r.id, r.value, r.note FROM readings_2028 AS r ORDER BY r.id;

-- sgl.query(name: ListDetachedReadings, command: many)
SELECT * FROM readings_2025 ORDER BY id;

-- sgl.query(name: NextReadingNumber, command: one)
SELECT nextval('reading_numbers') AS number;

-- sgl.query(name: ReadingNumbers, command: one)
SELECT last_value, log_cnt, is_called FROM reading_numbers;
