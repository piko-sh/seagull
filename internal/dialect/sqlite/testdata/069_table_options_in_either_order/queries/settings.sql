-- sgl.query(SettingLevels, many)
SELECT name, level FROM settings WHERE level > ?;

-- sgl.query(EnabledFlags, many)
SELECT name, enabled FROM flags WHERE enabled = ?;

-- sgl.query(SettingRowIDs, many)
SELECT rowid FROM settings;

-- sgl.query(FlagRowIDs, many)
SELECT rowid FROM flags;
