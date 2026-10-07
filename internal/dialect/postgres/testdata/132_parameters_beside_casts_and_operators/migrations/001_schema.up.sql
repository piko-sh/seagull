CREATE TABLE people (id BIGINT PRIMARY KEY, name text NOT NULL, born date NOT NULL);
CREATE TABLE reviews (id BIGINT PRIMARY KEY, listing_id text NOT NULL);
CREATE TABLE places (id BIGINT PRIMARY KEY, location point NOT NULL, mask integer NOT NULL, label text NOT NULL);
