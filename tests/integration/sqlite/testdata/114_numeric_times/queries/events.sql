-- sgl.query(name: Days, command: many)
SELECT id, date(created_at, 'unixepoch') AS day, datetime(created_at, 'unixepoch') AS moment,
       strftime('%Y-%m', created_at, 'unixepoch') AS month, julianday(created_at, 'unixepoch') AS julian_day,
       unixepoch(julian) AS from_julian, date(label) AS from_label
FROM events
ORDER BY id;

-- sgl.query(name: Flags, command: many)
SELECT id, if(flag, 'on', 'off') AS state, iif(flag, label) AS shown, if(flag = 1, 'one', flag = 2, 'two', 'many') AS count_word
FROM events
ORDER BY id;
