-- sgl.query(name: CreateUser, command: one)
INSERT INTO users (email, password_hash, tags) VALUES ($1, $2, $3) RETURNING email_domain, email_lower;

-- sgl.query(name: ListUsers, command: many)
SELECT * FROM users ORDER BY email;

-- sgl.query(name: Digest, command: one)
SELECT sha256(password_hash) AS digest FROM users WHERE email = $1;

-- sgl.query(name: Similar, command: many)
SELECT email, levenshtein(email, $1) AS distance, soundex(email) AS sound FROM users ORDER BY email;

-- sgl.query(name: Identifiers, command: one)
SELECT gen_random_ulid() IS NOT NULL AS has_ulid, unordered_unique_rowid() > 0 AS has_rowid;

-- sgl.query(name: AllTags, command: one)
SELECT array_cat_agg(tags ORDER BY email) AS tags FROM users;
