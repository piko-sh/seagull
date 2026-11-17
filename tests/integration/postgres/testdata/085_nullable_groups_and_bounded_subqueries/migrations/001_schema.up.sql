CREATE TABLE a (id bigint PRIMARY KEY, x bigint NOT NULL);
CREATE TABLE b (id bigint PRIMARY KEY, a_id bigint NOT NULL, y bigint NOT NULL);
CREATE TABLE c (id bigint PRIMARY KEY, b_id bigint NOT NULL, z bigint NOT NULL);
