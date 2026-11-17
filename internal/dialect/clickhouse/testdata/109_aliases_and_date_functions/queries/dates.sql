-- sgl.query(name: Buckets, command: many)
SELECT toStartOfInterval(at, INTERVAL 15 MINUTE) AS quarter_hour, toStartOfInterval(day, INTERVAL 1 MONTH) AS month_start,
       toStartOfInterval(at_precise, INTERVAL 100 MILLISECOND) AS tick, time_bucket(at, INTERVAL 1 HOUR) AS hour,
       count() AS events
FROM events
GROUP BY quarter_hour, month_start, tick, hour;

-- sgl.query(name: Calendar, command: many)
SELECT toYYYYMM(day) AS month_key, toYYYYMMDD(at) AS day_key, toMonday(day) AS week_start,
       toLastDayOfMonth(at) AS month_end, toStartOfFiveMinutes(at) AS five_minutes,
       toStartOfMillisecond(at_precise) AS millisecond, toDateTime(at_precise) AS whole_second, toDateTime(day) AS midnight,
       at + toIntervalHour(1) AS next_hour, fromUnixTimestamp(bytes) AS from_epoch
FROM events;
