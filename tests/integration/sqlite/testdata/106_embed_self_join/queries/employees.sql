-- sgl.query(name: ListEmployeesWithManager, command: many)
-- sgl.embed(employees, from: m, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
LEFT JOIN employees m ON m.id = e.manager_id
ORDER BY e.id;

-- sgl.query(name: ListReportingLines, command: many)
-- sgl.embed(employees, from: e, as: Report)
-- sgl.embed(employees, from: m, as: Manager)
SELECT e.id, e.name, m.id, m.name
FROM employees e
INNER JOIN employees m ON m.id = e.manager_id
ORDER BY e.id;
