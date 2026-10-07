-- sgl.query(name: InsertReading, command: exec)
INSERT INTO readings (id, station, taken_at, taken_on, reading, price, tally, valid, label, day_text, scores, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- sgl.query(name: StationSummary, command: many)
SELECT
    station,
    any_value(station) AS any_station,
    first(reading ORDER BY taken_at) AS opening_reading,
    last(taken_at ORDER BY taken_at) AS closing_time,
    arg_max(id, reading) AS peak_id,
    max_by(taken_on, tally) AS busiest_day,
    arg_max(id, reading, 2) AS top_ids,
    min_by(price, reading) AS cheapest_price,
    product(reading) AS reading_product,
    approx_count_distinct(id) AS distinct_ids,
    approx_quantile(tally, 0.5) AS approximate_median_tally,
    quantile(tally, 0.5) AS median_tally,
    quantile_cont(reading, 0.5) AS median_reading,
    histogram(tally) AS tally_histogram,
    approx_top_k(label, 1) AS frequent_labels,
    covar_pop(reading, tally) AS covariance,
    regr_count(reading, tally) AS fitted_rows,
    count_if(valid) AS valid_readings,
    count_star() AS readings,
    group_concat(label, '|') AS labels,
    string_agg(tally, ',' ORDER BY tally) AS tallies,
    weighted_avg(reading, tally) AS weighted_reading,
    mean(price) AS mean_price
FROM readings
GROUP BY station
ORDER BY station;

-- sgl.query(name: ReadingCalendar, command: many)
SELECT
    id,
    year(taken_at) AS taken_year,
    dayofweek(taken_on) AS taken_day_of_week,
    epoch_ms(taken_at) AS taken_epoch_ms,
    date_diff('day', taken_on, DATE '2024-12-31') AS days_left,
    date_add(taken_on, INTERVAL 1 DAY) AS next_day,
    time_bucket(INTERVAL '7 days', taken_on) AS week_bucket,
    datetrunc('month', taken_at) AS month_start,
    try_strptime(day_text, '%Y-%m-%d') AS parsed_day
FROM readings
ORDER BY id;

-- sgl.query(name: ReadingValues, command: many)
SELECT
    id,
    string_split(label, ',') AS label_parts,
    regexp_extract_all(label, '[0-9]+') AS label_numbers,
    levenshtein(station, label) AS edit_distance,
    list_position(scores, 3) AS position_of_three,
    list_sum(scores) AS score_total,
    list_avg(scores) AS score_average,
    list_max(scores) AS highest_score,
    list_histogram(scores) AS score_histogram,
    list_concat(scores, [tally]) AS scores_and_tally,
    json_extract(payload, '$.a') AS payload_a,
    json_extract_string(payload, '$.a') AS payload_a_text,
    fmod(reading, 2) AS reading_remainder,
    xor(tally, 1) AS flipped_tally,
    sqrt(0.25) AS root_quarter
FROM readings
ORDER BY id;
