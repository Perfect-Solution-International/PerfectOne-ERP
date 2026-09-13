ALTER TABLE users ADD COLUMN IF NOT EXISTS username text;
UPDATE users SET username = CASE email
  WHEN 'admin@grocerly.local' THEN 'admin'
  WHEN 'cashier@grocerly.local' THEN 'cashier'
  WHEN 'stock@grocerly.local' THEN 'stockmanager'
  ELSE split_part(email,'@',1)
END WHERE username IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_username_unique ON users(username);
