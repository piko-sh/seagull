CREATE TABLE scores (
    id INT AUTO_INCREMENT PRIMARY KEY,
    points INT NOT NULL DEFAULT 0
);

CREATE TABLE legacy_a (id INT);
CREATE TABLE legacy_b (id INT);
CREATE VIEW va AS SELECT id FROM legacy_a;
CREATE VIEW vb AS SELECT id FROM legacy_b;

DELIMITER //
CREATE FUNCTION bump_if(p INT) RETURNS INT
BEGIN
    DECLARE flag INT DEFAULT CASE WHEN p > 0 THEN 1 ELSE 0 END;
    IF p > 100 THEN
        SET flag = 2;
    END IF;
    UPDATE scores SET points = points + 1 WHERE points = p;
    RETURN flag;
END //
DELIMITER ;

CREATE FUNCTION label_of(p INT) RETURNS TEXT DETERMINISTIC
RETURN CONCAT('it''s ', `p`);

DROP VIEW IF EXISTS va, vb;
DROP TABLE legacy_a, legacy_b;
