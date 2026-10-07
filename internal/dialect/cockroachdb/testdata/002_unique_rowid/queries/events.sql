-- sgl.query(name: GenerateID, command: one)
SELECT unique_rowid() AS new_id;

-- sgl.query(name: GetEvent, command: one)
SELECT id, name, occurred_at FROM events WHERE id = $1;
