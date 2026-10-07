CREATE TABLE accounts (
    id INT PRIMARY KEY,
    name VARCHAR(40) NOT NULL,
    nickname VARCHAR(40) NULL,
    balance DECIMAL(10, 2) NOT NULL,
    overdraft INT NULL,
    opened_at DATETIME NOT NULL,
    profile JSON NOT NULL
);
