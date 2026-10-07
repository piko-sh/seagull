-- sgl.query(name: Catches, command: many)
-- sgl.column(id, type: integer, nullable: false)
-- sgl.column(species, type: varchar)
-- sgl.column(weight)
SELECT * FROM read_parquet('catches.parquet');

-- sgl.query(name: HeavyCatches, command: many)
-- sgl.column(id, type: integer, nullable: false)
-- sgl.column(weight, type: double)
SELECT c.id, weight FROM read_parquet('catches.parquet') c WHERE c.weight > $1;
