-- sgl.query(name: Days, command: many)
SELECT id, date(created_at, 'unixepoch') AS day, datetime(created_at, 'unixepoch', 'localtime') AS local_time,
       strftime('%Y-%m', created_at, 'unixepoch') AS month, julianday(created_at, 'unixepoch') AS julian_day,
       unixepoch(julian) AS from_julian, time(julian) AS clock
FROM events;

-- sgl.query(name: Flags, command: many)
SELECT if(flag, 'on', 'off') AS state, iif(flag, label) AS shown, if(flag = 1, 'one', flag = 2, 'two', 'many') AS count_word
FROM events;

-- sgl.query(name: Curves, command: one)
SELECT tanh(julian) AS squashed, cosh(julian) AS hyperbolic, unistr(label) AS decoded FROM events WHERE id = ?;
