package db

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func Connect() *pgx.Conn {
	conn, err := pgx.Connect(context.Background(), "postgres://rootuser:rootpassword@localhost:5432/nbeat")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}

	return conn
}
