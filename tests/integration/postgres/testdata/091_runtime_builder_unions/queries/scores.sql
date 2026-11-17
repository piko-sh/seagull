-- sgl.query(name: ScoresAndGuests, command: many, dynamic: runtime)
SELECT id, score FROM scores UNION ALL SELECT id, NULL FROM guests;
