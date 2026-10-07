INSERT INTO books (author_id, title) VALUES
    ((SELECT id FROM authors WHERE name = 'Ursula K. Le Guin'), 'The Dispossessed'),
    ((SELECT id FROM authors WHERE name = 'Terry Pratchett'), 'Night Watch');
