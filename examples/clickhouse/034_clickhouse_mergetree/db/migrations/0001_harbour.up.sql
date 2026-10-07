CREATE TABLE landings (
    id      UInt64,
    day     Date,
    boat    String,
    species String,
    kilos   UInt32
) ENGINE = MergeTree() ORDER BY (species, day, id);

CREATE TABLE berths (
    boat    String,
    berth   String,
    version UInt32
) ENGINE = ReplacingMergeTree(version) ORDER BY boat;

CREATE TABLE daily_catch (
    day     Date,
    species String,
    kilos   UInt64
) ENGINE = SummingMergeTree(kilos) ORDER BY (day, species);

CREATE TABLE species_stats (
    species  String,
    kilos    AggregateFunction(sum, UInt64),
    heaviest AggregateFunction(max, UInt32)
) ENGINE = AggregatingMergeTree() ORDER BY species;
