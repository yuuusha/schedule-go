INSERT INTO users (name) VALUES
    ('user1'),
    ('user2'),
    ('user3');

INSERT INTO resources (name, type, description, location, capacity, is_active) VALUES 
    ('room1', 'room', 'Test room 1', 'Building A', 10, TRUE),
    ('room2', 'room', 'Test room 2', 'Building A', 8, TRUE),
    ('room3', 'room', 'Test room 3', 'Building B', 6, TRUE),
    ('room4', 'room', 'Test room 4', 'Building B', 12, TRUE);

INSERT INTO bookings (resource_id, user_id, start_at, end_at, status)
SELECT 
    (SELECT id FROM resources WHERE name = 'room1'),
    (SELECT id FROM users WHERE name = 'user1'),
    '2026-12-02 12:00:00+00',
    '2026-12-02 14:00:00+00',
    'active';

INSERT INTO bookings (resource_id, user_id, start_at, end_at, status)
SELECT 
    (SELECT id FROM resources WHERE name = 'room1'),
    (SELECT id FROM users WHERE name = 'user2'),
    '2026-12-02 16:00:00+00',
    '2026-12-02 18:00:00+00',
    'active';

INSERT INTO bookings (resource_id, user_id, start_at, end_at, status)
SELECT 
    (SELECT id FROM resources WHERE name = 'room2'),
    (SELECT id FROM users WHERE name = 'user3'),
    '2026-12-02 10:00:00+00',
    '2026-12-02 11:30:00+00',
    'active';

INSERT INTO bookings (resource_id, user_id, start_at, end_at, status)
SELECT 
    (SELECT id FROM resources WHERE name = 'room3'),
    (SELECT id FROM users WHERE name = 'user3'),
    '2026-12-02 15:00:00+00',
    '2026-12-02 15:30:00+00',
    'active';

INSERT INTO bookings (resource_id, user_id, start_at, end_at, status)
SELECT 
    (SELECT id FROM resources WHERE name = 'room3'),
    (SELECT id FROM users WHERE name = 'user1'),
    '2026-12-02 16:00:00+00',
    '2026-12-02 16:30:00+00',
    'active';
