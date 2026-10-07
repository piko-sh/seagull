CREATE TABLE "order" (
    id BIGINT PRIMARY KEY,
    "group" INTEGER NOT NULL,
    "where" INTEGER,
    "check" VARCHAR NOT NULL,
    "constraint" VARCHAR NOT NULL,
    "primary" BOOLEAN NOT NULL,
    "select" VARCHAR
);
