CREATE TABLE people (id BIGINT PRIMARY KEY, name text NOT NULL, born date NOT NULL);
CREATE TABLE reviews (id BIGINT PRIMARY KEY, listing_id text NOT NULL);
CREATE TABLE flags (id BIGINT PRIMARY KEY, mask integer NOT NULL);
