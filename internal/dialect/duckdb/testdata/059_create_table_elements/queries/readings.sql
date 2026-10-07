-- sgl.query(name: ListReadings, command: many)
SELECT * FROM readings WHERE sensor = $1;
