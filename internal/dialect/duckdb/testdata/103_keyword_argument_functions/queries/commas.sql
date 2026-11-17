-- sgl.query(name: ExtractWithComma, command: many)
SELECT extract('day', taken) AS day_of_month FROM readings;

-- sgl.query(name: PositionWithComma, command: many)
SELECT position(field, 'a') AS found FROM readings;
