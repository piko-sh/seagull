CREATE TABLE tokens (id UInt64, owner Nullable(UUID), amount Nullable(Decimal(10, 2)), note Nullable(String)) ENGINE = MergeTree ORDER BY id;
