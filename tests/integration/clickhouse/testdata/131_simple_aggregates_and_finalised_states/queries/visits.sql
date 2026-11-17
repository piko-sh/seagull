-- sgl.query(RecordVisits, exec)
INSERT INTO visits SELECT toDate('2026-10-09'), 3, 'gull', ['harbour'], uniqState('ann'), sumState(toUInt32(7)), anyLastState('home');

-- sgl.query(RecordQuietDay, exec)
INSERT INTO visits SELECT toDate('2026-10-10'), 0, NULL, [], uniqState('bob'), sumState(toUInt32(1)), anyLastState('away');

-- sgl.query(AddHits, exec)
INSERT INTO visits (day, hits) VALUES ({day: Date}, {hits: SimpleAggregateFunction(sum, UInt64)});

-- sgl.query(Simple, many)
SELECT day, hits, longest, pages FROM visits FINAL ORDER BY day;

-- sgl.query(Finalised, many)
SELECT day, finalizeAggregation(visitors) AS visitors, finalizeAggregation(total) AS total,
    finalizeAggregation(last_page) AS last_page
FROM visits FINAL ORDER BY day;

-- sgl.query(Merged, one)
SELECT uniqMerge(visitors) AS visitors, sumMerge(total) AS total, sum(hits) AS hits FROM visits;
