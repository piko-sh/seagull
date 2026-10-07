-- sgl.query(name: GetByActiveStatus, command: many)
-- ?1 as sgl.param(is_active)
SELECT id, name FROM flags WHERE active = ?1 ORDER BY name;
