ALTER TABLE sales ADD COLUMN IF NOT EXISTS discount numeric(14,2) NOT NULL DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS payment_method text NOT NULL DEFAULT 'cash';
ALTER TABLE sales ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'completed' CHECK(status IN('completed','cancelled','returned'));
ALTER TABLE sales ADD COLUMN IF NOT EXISTS cancelled_at timestamptz;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS cancelled_by uuid REFERENCES users(id);
CREATE TABLE IF NOT EXISTS sale_returns(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),sale_id uuid NOT NULL REFERENCES sales(id),return_no text NOT NULL UNIQUE,reason text NOT NULL,returned_by uuid REFERENCES users(id),total numeric(14,2) NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE IF NOT EXISTS sale_return_items(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),return_id uuid NOT NULL REFERENCES sale_returns(id) ON DELETE CASCADE,sale_item_id uuid NOT NULL REFERENCES sale_items(id),quantity numeric(14,3) NOT NULL,total numeric(14,2) NOT NULL);
