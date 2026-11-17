-- sgl.query(name: Finalised, command: many, dynamic: runtime)
SELECT id, kind FROM events FINAL

-- sgl.query(name: Sampled, command: many, dynamic: runtime)
SELECT id, kind FROM events SAMPLE 1

-- sgl.query(name: Unrolled, command: many, dynamic: runtime)
SELECT id, tag FROM events ARRAY JOIN tags AS tag

-- sgl.query(name: Prefiltered, command: many, dynamic: runtime)
SELECT id, kind FROM events PREWHERE version > 0
