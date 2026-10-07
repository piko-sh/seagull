-- sgl.query(name: Dearer, command: many)
SELECT id, fare * 2 AS doubled FROM trips WHERE doubled > ? ORDER BY doubled DESC;
