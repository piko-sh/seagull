CREATE TABLE orders (id UInt64, price Float64, qty Int64, note Nullable(String), basket String, tags Array(String), weights Array(UInt32), counts Map(String, UInt32)) ENGINE = MergeTree ORDER BY id;
