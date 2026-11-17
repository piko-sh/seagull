CREATE TABLE shifts (
    id INT AUTO_INCREMENT PRIMARY KEY,
    starts_at TIME NOT NULL,
    season YEAR NOT NULL,
    flags BIT(8) NOT NULL,
    ends_at TIME
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
