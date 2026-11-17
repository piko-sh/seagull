-- sgl.query(name: Highest, command: one)
WITH max(id) AS highest SELECT highest FROM readings;

-- sgl.query(name: Scaled, command: many)
WITH 2 AS factor SELECT factor * id AS scaled, id + 1 AS following FROM readings WHERE id > factor;

-- sgl.query(name: Bounded, command: many)
WITH {limit:UInt32} AS bound SELECT id FROM readings WHERE id < bound;
