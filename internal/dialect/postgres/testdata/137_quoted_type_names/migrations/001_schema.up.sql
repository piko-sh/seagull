CREATE SCHEMA "Shapes";

CREATE TYPE "Mood" AS ENUM ('Happy', 'Sad');

CREATE TYPE mood AS ENUM ('calm', 'tense', 'bored');

CREATE TYPE "Shapes"."Pair" AS (
    left_value integer,
    right_value integer
);

CREATE TYPE "Shapes".pair AS (
    label text,
    amount bigint
);

CREATE TYPE "Point" AS (
    x integer,
    y integer
);

CREATE TABLE readings (
    id integer PRIMARY KEY,
    quoted_mood "Mood" NOT NULL,
    plain_mood mood NOT NULL,
    shouted_mood MOOD NOT NULL,
    quoted_moods "Mood"[] NOT NULL,
    quoted_pair "Shapes"."Pair" NOT NULL,
    plain_pair "Shapes".pair NOT NULL,
    corner "Point" NOT NULL,
    spot point NOT NULL
);

CREATE FUNCTION "Swap"(value "Shapes"."Pair")
RETURNS "Shapes"."Pair"
LANGUAGE sql IMMUTABLE
AS $$
    SELECT ROW(value.right_value, value.left_value)::"Shapes"."Pair";
$$;

CREATE FUNCTION settle(value "Mood")
RETURNS mood
LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE WHEN value = 'Happy' THEN 'calm'::mood ELSE 'tense'::mood END;
$$;

CREATE FUNCTION unsettle(value mood)
RETURNS "Mood"
LANGUAGE sql IMMUTABLE
AS $$
    SELECT CASE WHEN value = 'calm' THEN 'Happy'::"Mood" ELSE 'Sad'::"Mood" END;
$$;
