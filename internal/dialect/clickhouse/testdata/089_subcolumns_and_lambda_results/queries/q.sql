-- sgl.query(Subcolumns, many)
SELECT tags.size0 AS tag_count, grid.size1 AS row_lengths, ringings.keys AS ringers, ringings.values AS ring_counts,
       ringings.size0 AS ringer_count, note.null AS note_missing, nicknames.null AS nickname_missing,
       flights.colony AS colonies, flights.minutes AS minutes
FROM gulls;

-- sgl.query(LambdaResults, many)
SELECT arrayMap(t -> length(t), tags) AS tag_lengths, arrayMap((t, s) -> concat(t, toString(s)), tags, sightings) AS labelled,
       arrayFilter(t -> t != '', tags) AS named, arrayFirst(t -> t != '', tags) AS first_named,
       arrayFirstOrNull(t -> t != '', tags) AS maybe_named, arrayCount(s -> s > 1, sightings) AS busy_days,
       arrayExists(s -> s > 1, sightings) AS any_busy, arraySum(s -> s * 2, sightings) AS doubled_total,
       arrayMax(t -> length(t), tags) AS longest, arrayFold((total, s) -> total + s, sightings, toUInt64(0)) AS folded,
       arrayCumSum(s -> s * 2, sightings) AS running, arrayMap(n -> n, nicknames) AS same_nicknames,
       arraySort(t -> length(t), tags) AS by_length, nicknames[1] AS first_nickname
FROM gulls;
