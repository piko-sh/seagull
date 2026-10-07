-- sgl.query(name: ListUsers, command: many)
SELECT * FROM users;

-- sgl.query(name: CreateUser, command: one)
INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id, email_domain;

-- sgl.query(name: PasswordHash, command: one)
SELECT password_hash, sha256(password_hash) AS digest FROM users WHERE email = $1;

-- sgl.query(name: Similar, command: many)
SELECT email, levenshtein(email, $1) AS distance, soundex(email) AS sound FROM users;

-- sgl.query(name: NewIdentifiers, command: one)
SELECT gen_random_ulid() AS ulid, unordered_unique_rowid() AS rowid, uuid_generate_v4() AS uuid;

-- sgl.query(name: AllTags, command: one)
SELECT array_cat_agg(tags) AS tags FROM users;

-- sgl.query(name: CountA, command: one)
SELECT regexp_count(email, 'a') AS a_count FROM users WHERE email = $1;
