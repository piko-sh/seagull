-- sgl.query(name: InsertDepartment, command: exec)
INSERT INTO departments (id, name) VALUES (?, ?);

-- sgl.query(name: InsertEmployee, command: exec)
INSERT INTO employees (id, name, dept_id) VALUES (?, ?, ?);

-- sgl.query(name: ListEmployees, command: many)
SELECT id, name, dept_id FROM employees ORDER BY id;

-- sgl.query(name: DeleteDepartment, command: exec)
DELETE FROM departments WHERE id = ?;

-- sgl.query(name: CountEmployees, command: one)
SELECT COUNT(*) AS total FROM employees;
