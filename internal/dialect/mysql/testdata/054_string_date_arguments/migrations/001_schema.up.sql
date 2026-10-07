CREATE TABLE bookings (
    id BIGINT PRIMARY KEY,
    label VARCHAR(40) NOT NULL,
    starts_on DATE NOT NULL,
    starts_at DATETIME NOT NULL,
    stamped_at DATETIME(3) NOT NULL,
    opens TIME NOT NULL,
    raw_date VARCHAR(20) NOT NULL
);
