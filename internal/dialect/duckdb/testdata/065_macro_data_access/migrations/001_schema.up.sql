CREATE SEQUENCE landing_ids;
CREATE TABLE landings (id INTEGER PRIMARY KEY, harbour VARCHAR NOT NULL, species VARCHAR NOT NULL, kg INTEGER NOT NULL, grade VARCHAR);
CREATE MACRO tonnes(kg) AS kg / 1000.0;
CREATE MACRO shout(word) AS upper(word) || '!';
CREATE MACRO next_landing_id() AS nextval('landing_ids');
CREATE MACRO harbour_landings(name) AS TABLE SELECT species, kg, grade FROM landings WHERE harbour = name;
