-- sgl.query(name: GetConfig, command: one)
SELECT id, name, CAST(settings AS VARCHAR) AS settings FROM configs WHERE id = $1;

-- sgl.query(name: ListConfigs, command: many)
SELECT id, name, CAST(settings AS VARCHAR) AS settings FROM configs ORDER BY id;
