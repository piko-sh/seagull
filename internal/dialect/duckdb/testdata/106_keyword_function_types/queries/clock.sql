-- sgl.query(name: KeywordForms, command: one)
SELECT current_time AS time_with_zone, current_timestamp AS stamp_with_zone, localtime AS local_time,
       localtimestamp AS local_stamp, current_date AS today, current_role AS role_name,
       session_user AS session_name
FROM events WHERE id = $1;

-- sgl.query(name: CallForms, command: one)
SELECT current_date() AS today, current_user() AS user_name, current_role() AS role_name,
       session_user() AS session_name, current_schema() AS schema_name, current_catalog() AS catalog_name,
       get_current_time() AS time_with_zone
FROM events WHERE id = $1;
