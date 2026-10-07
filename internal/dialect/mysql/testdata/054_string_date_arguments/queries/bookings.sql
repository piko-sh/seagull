-- sgl.query(name: ReadStringDates, command: many)
SELECT
    id,
    YEAR('2024-01-01') AS literal_year,
    MONTH(raw_date) AS text_month,
    DATEDIFF(NOW(), '2024-01-01') AS days_since,
    DATE_ADD('2024-01-01', INTERVAL 1 DAY) AS next_day,
    DATE_SUB(raw_date, INTERVAL 1 DAY) AS previous_day,
    ADDDATE(starts_on, 1) AS following_date,
    ADDTIME(opens, '01:00:00') AS later_opening,
    SUBTIME('10:00:00', '01:00:00') AS earlier_text,
    TIMESTAMPADD(DAY, 1, '2024-01-01') AS stamped_next_day,
    TIMESTAMPDIFF(DAY, '2024-01-01', starts_at) AS days_after,
    DATE_FORMAT('2024-01-01', '%Y') AS formatted,
    UNIX_TIMESTAMP('2024-01-01') AS literal_epoch,
    UNIX_TIMESTAMP(starts_at) AS epoch,
    UNIX_TIMESTAMP(stamped_at) AS precise_epoch,
    HOUR('10:00:00') AS literal_hour,
    DATE('2024-01-01 10:00:00') AS literal_date,
    TIMESTAMP(raw_date) AS text_timestamp,
    LAST_DAY(raw_date) AS month_end,
    YEAR(20240101) AS number_year,
    EXTRACT(YEAR FROM raw_date) AS extracted_year
FROM bookings
WHERE starts_on > '2024-01-01'
    AND starts_at BETWEEN '2024-01-01' AND '2024-12-31'
    AND starts_on IN ('2024-01-01', '2024-01-02')
    AND opens < '18:00:00';

-- sgl.query(name: BookingsInYear, command: many)
SELECT id
FROM bookings
WHERE YEAR(starts_on) = ? AND DATE_ADD(?, INTERVAL 1 DAY) > starts_at;
