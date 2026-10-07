CREATE SCHEMA billing;

CREATE TYPE mood AS ENUM ('happy', 'sad');

CREATE DOMAIN email_address AS TEXT NOT NULL CHECK (VALUE ~ '@');

CREATE DOMAIN nickname AS VARCHAR(40) DEFAULT 'anon' CHECK (char_length(VALUE) > 1);

CREATE DOMAIN billing.cents AS BIGINT CONSTRAINT positive CHECK (VALUE >= 0);

CREATE DOMAIN contact_email AS email_address;

CREATE DOMAIN tags AS TEXT[];

CREATE DOMAIN feeling AS mood NOT NULL;

CREATE TABLE people (
    id SERIAL PRIMARY KEY,
    email email_address,
    backup_email contact_email,
    nickname nickname,
    balance billing.cents NOT NULL,
    labels tags,
    current_feeling feeling
);

CREATE DOMAIN retired AS INTEGER;

DROP DOMAIN retired;

CREATE TABLE mailing (id INTEGER PRIMARY KEY, recipients email_address[]);
