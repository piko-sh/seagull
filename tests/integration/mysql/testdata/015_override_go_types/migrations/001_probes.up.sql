CREATE TABLE probes (
    id INT PRIMARY KEY,
    label VARCHAR(64) NOT NULL,
    amount BIGINT NOT NULL,
    ratio DOUBLE NOT NULL,
    flag BOOLEAN NOT NULL,
    note TEXT NOT NULL,
    payload VARBINARY(16) NOT NULL,
    optional_label VARCHAR(64) NULL,
    optional_amount BIGINT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
