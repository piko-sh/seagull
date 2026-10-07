INSERT INTO menu (dish, price_pence) VALUES
    ('cod and chips', 950),
    ('haddock and chips', 1020),
    ('mushy peas', 250)
ON CONFLICT (dish) DO NOTHING;
