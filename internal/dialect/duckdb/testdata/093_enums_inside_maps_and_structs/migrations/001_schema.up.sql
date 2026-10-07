CREATE TYPE mood AS ENUM ('calm', 'stormy');

CREATE TABLE logs (
    id INTEGER PRIMARY KEY,
    moods MAP(VARCHAR, mood),
    counts MAP(mood, INTEGER),
    entry STRUCT(note VARCHAR, feeling mood),
    history STRUCT(feelings mood[]),
    entries STRUCT(feeling mood)[],
    tagged UNION(feeling mood, note VARCHAR)
);
