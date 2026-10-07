CREATE TABLE readings (
    id BIGINT PRIMARY KEY,
    sensor VARCHAR COLLATE NOCASE NOT NULL,
    celsius DOUBLE NOT NULL CHECK (celsius > -273.15),
    fahrenheit DOUBLE GENERATED ALWAYS AS (celsius * 9 / 5 + 32) VIRTUAL,
    kelvin AS (celsius + 273.15),
    taken_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT current_timestamp,
    tags VARCHAR[] NOT NULL DEFAULT [],
    CONSTRAINT readings_sensor_unique UNIQUE (sensor, taken_at),
    CHECK (celsius < 1000)
);
