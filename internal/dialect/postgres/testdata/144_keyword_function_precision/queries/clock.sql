-- sgl.query(name: Precise, command: one)
SELECT current_time(2) AS time_with_zone, current_timestamp(3) AS stamp_with_zone, localtime(1) AS local_time,
       localtimestamp(0) AS local_stamp, current_schema() AS schema_name
FROM events WHERE id = $1;
