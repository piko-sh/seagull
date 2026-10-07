CREATE TABLE boats (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(100),
    length INT,
    legacy TEXT,
    INDEX idx_name (name)
) ENGINE=InnoDB;

ALTER TABLE boats ADD COLUMN crew INT NOT NULL DEFAULT 1, DROP INDEX idx_name, MODIFY COLUMN name VARCHAR(200) NOT NULL, DROP COLUMN legacy;

ALTER TABLE boats CHANGE COLUMN length length_m DECIMAL(6, 2) NOT NULL;

CREATE TABLE boat_names AS SELECT id, name FROM boats;
