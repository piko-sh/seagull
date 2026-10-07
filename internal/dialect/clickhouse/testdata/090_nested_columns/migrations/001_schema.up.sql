CREATE TABLE flights (
    id UInt64,
    legs Nested(colony String, minutes Nullable(UInt16)),
    note String
) ENGINE = MergeTree ORDER BY id;

ALTER TABLE flights ADD COLUMN crew Nested(name String, role String);
ALTER TABLE flights ADD COLUMN legs.altitude Array(UInt32);
ALTER TABLE flights RENAME COLUMN `crew.role` TO `crew.duty`;
ALTER TABLE flights DROP COLUMN crew;

CREATE TABLE routes (id UInt64, stops Nested(colony String, minutes UInt16)) ENGINE = MergeTree ORDER BY id;
