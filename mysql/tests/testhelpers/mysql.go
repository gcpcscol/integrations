package testhelpers

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartMySQLContainer starts a mysql:8 container attached to net with the given
// network alias.  It creates a database named "testdb" with root password "secret".
//
// The container is seeded with:
//   - A "users" table with three rows
//   - A "orders" table with two rows referencing users
//   - A stored procedure "get_users"
//   - A BEFORE INSERT trigger on "orders"
//
// The container is automatically terminated when the test ends.
func StartMySQLContainer(ctx context.Context, t *testing.T, net *testcontainers.DockerNetwork, alias string) testcontainers.Container {
	t.Helper()

	req := testcontainers.ContainerRequest{
		Image: "mysql:8",
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "secret",
			"MYSQL_DATABASE":      "testdb",
		},
		Networks:       []string{net.Name},
		NetworkAliases: map[string][]string{net.Name: {alias}},
		// Wait until MySQL is accepting connections on port 3306.
		WaitingFor: wait.ForLog("port: 3306  MySQL Community Server"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("start mysql container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	return container
}

// SeedMySQL populates testdb with representative data including tables,
// a stored procedure, and a trigger. This covers the most common mysqldump
// content types and gives meaningful row counts to verify after restore.
func SeedMySQL(ctx context.Context, t *testing.T, container testcontainers.Container) {
	t.Helper()

	seed := `
CREATE TABLE users (
  id   INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(255) NOT NULL
);
INSERT INTO users (name) VALUES ('alice'), ('bob'), ('carol');

CREATE TABLE orders (
  id      INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  amount  DECIMAL(10,2) NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id)
);
INSERT INTO orders (user_id, amount) VALUES (1, 99.99), (2, 149.50), (3, 9.99);

DROP PROCEDURE IF EXISTS get_users;
CREATE PROCEDURE get_users() BEGIN SELECT * FROM users; END;

DROP TRIGGER IF EXISTS before_order_insert;
CREATE TRIGGER before_order_insert
  BEFORE INSERT ON orders
  FOR EACH ROW
  SET NEW.amount = IF(NEW.amount < 0, 0, NEW.amount);
`
	ExecOK(ctx, t, container,
		"mysql", "-uroot", "-psecret", "testdb", "-e", seed)
}
