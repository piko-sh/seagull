-- sgl.query(name: InsertBooking, command: exec)
INSERT INTO bookings (id, label, starts_on, starts_at, stamped_at, opens, raw_date) VALUES (?, ?, ?, ?, ?, ?, ?);

-- sgl.query(name: ReadStringDates, command: many)
SELECT
    id,
    YEAR(raw_date) AS text_year,
    MONTH('2024-03-15') AS literal_month,
    DATEDIFF(starts_on, '2024-01-01') AS days_since_new_year,
    DATE_ADD(raw_date, INTERVAL 1 DAY) AS next_day,
    DATE_SUB('2024-03-01', INTERVAL 1 DAY) AS previous_day,
    ADDDATE(starts_on, 1) AS following_date,
    ADDTIME(opens, '01:00:00') AS later_opening,
    SUBTIME('10:00:00', '01:00:00') AS earlier_text,
    TIMESTAMPADD(HOUR, 1, raw_date) AS next_hour,
    TIMESTAMPDIFF(DAY, raw_date, starts_at) AS days_after,
    DATE_FORMAT(raw_date, '%Y/%m') AS formatted,
    UNIX_TIMESTAMP('2024-01-01 00:00:00') AS literal_epoch,
    UNIX_TIMESTAMP(raw_date) AS text_epoch,
    UNIX_TIMESTAMP(stamped_at) AS precise_epoch,
    HOUR('10:30:00') AS literal_hour,
    DATE(raw_date) AS text_date,
    TIMESTAMP(raw_date) AS text_timestamp,
    LAST_DAY(raw_date) AS month_end,
    YEAR(20240101) AS number_year,
    EXTRACT(YEAR FROM raw_date) AS extracted_year
FROM bookings
WHERE starts_on >= '2024-01-01' AND starts_at BETWEEN '2024-01-01' AND '2025-01-01'
ORDER BY id;

-- sgl.query(name: CountBookingsInYear, command: one)
SELECT COUNT(*) AS bookings
FROM bookings
WHERE YEAR(starts_on) = ? AND starts_on IN ('2024-02-01', '2024-02-29') AND opens < '12:00:00';

-- sgl.query(name: ShiftParameter, command: one)
SELECT DATE_ADD(?, INTERVAL 1 DAY) AS shifted;
