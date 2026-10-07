-- sgl.query(name: SearchEmployees, command: many)
-- $1 as sgl.param(department, optional: true)
-- $2 as sgl.param(min_level, optional: true)
-- $3 as sgl.param(active, optional: true)
SELECT id, name, department, level, active FROM employees WHERE ($1::text IS NULL OR department = $1) AND ($2::int IS NULL OR level >= $2) AND ($3::int IS NULL OR active = $3) ORDER BY id ASC;
