-- sgl.query(name: GetWidget, command: one)
SELECT * FROM widgets WHERE id = ?;

-- sgl.query(name: GetWidgetQualified, command: one)
SELECT w.* FROM widgets w WHERE w.id = ?;
