-- sgl.query(name: Audit, command: one)
SELECT audited($1) AS id;

-- sgl.query(name: AuditAtomic, command: one)
SELECT audited_atomic($1) AS id;

-- sgl.query(name: Tidy, command: one)
SELECT tidied($1) AS name;

-- sgl.query(name: Describe, command: one)
SELECT described($1) AS name;
