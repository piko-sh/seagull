-- sgl.query(name: Bookings, command: many)
SELECT id, lower(booked) AS starts, upper(booked) AS ends, isempty(seats) AS unseated,
       lower_inf(booked) AS open_start, tstzrange($1, $2, '[)') AS window
FROM events;

-- sgl.query(name: Moods, command: one)
SELECT enum_first(NULL::mood) AS lowest, enum_last(feeling) AS highest, enum_range(NULL::mood) AS every_mood
FROM events
WHERE id = $1;
