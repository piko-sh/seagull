-- sgl.query(name: EmptyRole, command: one)
SELECT current_role() AS role_name FROM events;

-- sgl.query(name: EmptyCatalog, command: one)
SELECT current_catalog() AS catalog_name FROM events;
