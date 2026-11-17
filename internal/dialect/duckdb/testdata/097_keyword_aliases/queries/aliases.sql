-- sgl.query(name: BareName, command: many)
SELECT k name FROM a;

-- sgl.query(name: BareLabel, command: many)
SELECT lower(CAST(k AS VARCHAR)) label FROM a;

-- sgl.query(name: AsName, command: many)
SELECT k AS name FROM a;

-- sgl.query(name: BareTotal, command: many)
SELECT k total FROM a;
