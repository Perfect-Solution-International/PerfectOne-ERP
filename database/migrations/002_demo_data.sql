INSERT INTO users (name,email,password_hash,role) VALUES
('System Administrator','admin@grocerly.local',crypt('Admin@123',gen_salt('bf')),'super_admin'),
('Kasun Cashier','cashier@grocerly.local',crypt('Cashier@123',gen_salt('bf')),'cashier'),
('Nimal Stock Manager','stock@grocerly.local',crypt('Stock@123',gen_salt('bf')),'stock_manager')
ON CONFLICT (email) DO NOTHING;
INSERT INTO categories(name) VALUES ('Grains'),('Dairy'),('Cooking'),('Essentials'),('Beverages') ON CONFLICT(name) DO NOTHING;
INSERT INTO suppliers(name,phone,email,balance) VALUES ('Ceylon Distributors','0115551234','orders@ceylondist.local',24500),('Lanka Wholesale','0115554321','sales@lankawholesale.local',12500) ON CONFLICT DO NOTHING;
INSERT INTO customers(name,phone,credit_limit,balance) VALUES ('Walk-in Customer','0000000000',0,0),('Amal Perera','0771234567',10000,2450),('Nadeesha Silva','0717654321',15000,0) ON CONFLICT(phone) DO NOTHING;
INSERT INTO products(name,sku,barcode,category_id,purchase_price,selling_price,stock_quantity,minimum_stock)
SELECT v.name,v.sku,v.barcode,c.id,v.cost,v.price,v.stock,v.minimum FROM (VALUES
('Samba Rice 1kg','RICE-001','479100100001',205::numeric,245::numeric,42::numeric,15::numeric,'Grains'),
('Anchor Milk Powder 400g','MILK-400','479100100002',1120::numeric,1280::numeric,8::numeric,10::numeric,'Dairy'),
('Coconut Oil 1L','OIL-001','479100100003',760::numeric,890::numeric,25::numeric,8::numeric,'Cooking'),
('Lanka Salt 400g','SALT-400','479100100004',125::numeric,160::numeric,56::numeric,20::numeric,'Essentials'),
('Dilmah Tea 100g','TEA-100','479100100005',515::numeric,620::numeric,5::numeric,8::numeric,'Beverages')) AS v(name,sku,barcode,cost,price,stock,minimum,category) JOIN categories c ON c.name=v.category
ON CONFLICT(sku) DO NOTHING;
