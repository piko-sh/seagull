-- sgl.query(name: Parts, command: many)
SELECT
    extract(day FROM taken) AS day_of_month,
    extract(epoch FROM taken) AS epoch_seconds,
    date_part('second', taken) AS whole_seconds,
    date_part('JULIAN', taken) AS julian_day,
    datepart('epoch', taken) AS epoch_again,
    date_part(field, taken) AS chosen_part,
    extract(hour FROM span) AS span_hours
FROM readings;
