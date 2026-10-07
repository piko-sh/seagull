CREATE TYPE mood AS ENUM ('happy', 'sad');
ALTER TYPE mood ADD VALUE IF NOT EXISTS 'sad';
ALTER TYPE mood ADD VALUE 'calm' BEFORE 'sad';
ALTER TYPE mood ADD VALUE IF NOT EXISTS 'angry' AFTER 'happy';
ALTER TYPE mood ADD VALUE IF NOT EXISTS 'happy';
CREATE TABLE feelings (id integer PRIMARY KEY, mood mood NOT NULL, moods mood[] NOT NULL);
