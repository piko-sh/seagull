-- sgl.query(name: EmptyDate, command: one)
SELECT current_date() AS today FROM events;

-- sgl.query(name: EmptyUser, command: one)
SELECT current_user() AS who FROM events;

-- sgl.query(name: DatePrecision, command: one)
SELECT current_date(1) AS today FROM events;
