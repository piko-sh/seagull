CREATE TYPE IF NOT EXISTS community_role AS ENUM ('creator', 'admin');
CREATE TYPE IF NOT EXISTS community_role AS ENUM ('other');
ALTER TYPE community_role ADD VALUE IF NOT EXISTS 'admin';
CREATE TABLE IF NOT EXISTS members (community_id INT8, user_id INT8, role community_role NOT NULL, PRIMARY KEY (community_id, user_id));
CREATE TABLE IF NOT EXISTS members (id INT8 PRIMARY KEY);
ALTER TABLE members ADD COLUMN IF NOT EXISTS role community_role;
ALTER TABLE IF EXISTS gone ADD COLUMN extra INT8;
CREATE VIEW member_view AS SELECT * FROM members;
