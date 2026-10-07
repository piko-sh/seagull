CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE items (id bigint PRIMARY KEY, embedding vector(3) NOT NULL, label text NOT NULL);
