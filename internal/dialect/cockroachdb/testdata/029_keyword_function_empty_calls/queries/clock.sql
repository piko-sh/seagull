-- sgl.query(name: EmptyCalls, command: one)
SELECT current_date() AS today, current_time() AS time_with_zone, current_timestamp() AS stamp_with_zone,
       localtime() AS local_time, localtimestamp() AS local_stamp, current_user() AS who, session_user() AS session_name,
       current_schema() AS schema_name
FROM events WHERE id = $1;
