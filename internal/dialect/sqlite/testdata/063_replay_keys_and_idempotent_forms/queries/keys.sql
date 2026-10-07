-- sgl.query(name: ListKeys, command: many)
SELECT p.code, d.code AS declared_code, pa.left_id, r.left_id AS rowless_left_id, s.code AS strict_code,
       a.id AS aliased_id, t.id AS table_aliased_id, de.id AS descending_id, si.id AS short_int_id
FROM plain p, declared d, pairs pa, rowless r, strict_keys s, aliased a, table_aliased t, descending de, short_int si;

-- sgl.query(name: ListPlainView, command: many)
SELECT * FROM plain_view;
