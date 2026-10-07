-- sgl.query(name: GetLogCount, command: one)
SELECT count_logs() AS total;

-- sgl.query(name: ListLogs, command: many)
SELECT id, message, created_at FROM logs;
