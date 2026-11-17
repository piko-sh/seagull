-- sgl.query(name: ListPeople, command: many)
SELECT id, email, backup_email, nickname, balance, labels, current_feeling FROM people;

-- sgl.query(name: FindByEmail, command: many)
SELECT id FROM people WHERE email = $1::email_address;
