CREATE TABLE visits (
    day Date,
    hits SimpleAggregateFunction(sum, UInt64),
    longest SimpleAggregateFunction(max, Nullable(String)),
    pages SimpleAggregateFunction(groupArrayArray, Array(String)),
    visitors AggregateFunction(uniq, String),
    total AggregateFunction(sum, UInt32),
    last_page AggregateFunction(anyLast, String)
) ENGINE = AggregatingMergeTree ORDER BY day;
