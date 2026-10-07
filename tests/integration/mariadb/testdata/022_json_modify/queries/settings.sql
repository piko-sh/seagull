-- sgl.query(name: InsertSetting, command: exec)
INSERT INTO settings (name, config) VALUES (?, ?);

-- sgl.query(name: GetSetting, command: one)
SELECT id, name, config FROM settings WHERE id = ?;

-- sgl.query(name: SetConfigField, command: exec)
UPDATE settings SET config = JSON_SET(config, ?, ?) WHERE id = ?;

-- sgl.query(name: ReplaceConfigField, command: exec)
UPDATE settings SET config = JSON_REPLACE(config, '$.theme', ?) WHERE id = ?;

-- sgl.query(name: RemoveConfigField, command: exec)
UPDATE settings SET config = JSON_REMOVE(config, ?) WHERE id = ?;
