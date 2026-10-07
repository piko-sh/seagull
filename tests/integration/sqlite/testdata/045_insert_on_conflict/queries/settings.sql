-- sgl.query(name: UpsertSetting, command: exec)
-- ?1 as sgl.param(key)
-- ?2 as sgl.param(value)
INSERT INTO settings (key, value) VALUES (?1, ?2)
ON CONFLICT (key) DO UPDATE SET value = ?2, updated_count = settings.updated_count + 1;
