package rule

import "testing"

func TestValidateCreateTable(t *testing.T) {
	r := Rule{
		Pattern: "https://shop.example/api/orders/*",
		Table:   "shop_orders",
		Fields: []Field{
			{Name: "order_id", Type: "string", From: "$.id", Required: true},
			{Name: "amount", Type: "number", From: "$.total"},
		},
	}
	ok, err := CreateTableSQL(r, DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCreateTable(ok, r, DialectPostgres); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"DROP TABLE shop_orders",
		"CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT); DROP TABLE shop_orders",
		"CREATE TABLE IF NOT EXISTS other (order_id TEXT, amount DOUBLE PRECISION, _captured_at TEXT, _source_url TEXT, _rule_id TEXT, _rule_version INTEGER)",
		"CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT) -- comment",
		"CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT, amount DOUBLE PRECISION, _captured_at TEXT, _source_url TEXT, _rule_id TEXT)",
		"ATTACH DATABASE 'x' AS y; CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT)",
	}
	for _, sql := range cases {
		if err := ValidateCreateTable(sql, r, DialectPostgres); err == nil {
			t.Fatalf("accepted unsafe sql: %s", sql)
		}
	}
	sqlite := "CREATE TABLE IF NOT EXISTS shop_orders (order_id TEXT, amount REAL, _captured_at TEXT, _source_url TEXT, _rule_id TEXT, _rule_version INTEGER)"
	if err := ValidateCreateTable(sqlite, r, DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if err := ValidateCreateTable("CREATE TABLE IF NOT EXISTS shop_orders (order_id JSONB, amount REAL, _captured_at TEXT, _source_url TEXT, _rule_id TEXT, _rule_version INTEGER)", r, DialectSQLite); err == nil {
		t.Fatal("sqlite must reject jsonb")
	}
}

func TestTableName(t *testing.T) {
	r := Rule{Pattern: "https://example.com/*", Table: "Bad-Name", Fields: []Field{{Name: "id", Type: "string", From: "$.id"}}}
	if err := r.Validate(); err == nil {
		t.Fatal("expected table name rejection")
	}
}
