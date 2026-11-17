-- sgl.query(name: FullCte, command: many)
WITH t (x, y) AS (SELECT k, v FROM a) SELECT x, y FROM t;

-- sgl.query(name: ShortCte, command: many)
WITH t (x) AS (SELECT k, v FROM a) SELECT x, v FROM t;

-- sgl.query(name: SurplusCte, command: many)
WITH t (x, y, z) AS (SELECT k, v FROM a) SELECT x FROM t;

-- sgl.query(name: FullDerived, command: many)
SELECT d.x, d.y FROM (SELECT k, v FROM a) AS d (x, y);
