// Command seed inserts the demo agents and a set of historic visits so the
// list is never empty on stage. It is idempotent: running it twice changes
// nothing.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/db"
	"github.com/spiderocious/devfest-akure-2026/fieldkit/backend/internal/ids"
)

type agent struct{ id, phone, name string }

var agents = []agent{
	{"ag_01AYOMIDE000000000000000000", "08030000001", "Ayomide Bello"},
	{"ag_01CHUKWUEMEKA00000000000000", "08030000002", "Chukwuemeka Obi"},
	{"ag_01NNEKA00000000000000000000", "08030000003", "Nneka Okafor"},
}

var shops = []string{"Mama Tunde Provisions", "Oja Oba Electronics", "Iyaloja Fabrics", "Akure Fresh Foods",
	"De Blessing Pharmacy", "Olumide Phones & Repairs", "Grace Tailoring", "Sunrise Bakery", "Kehinde Cement Depot",
	"Royal Cuts Barbing", "Precious Cosmetics", "Oluwaseun Bookshop", "Fatima Spices", "Chidi Auto Parts",
	"Bola Frozen Foods", "Emeka Furniture", "Adaeze Boutique", "Segun Plumbing Supplies", "Halima Grains", "Temidayo Prints"}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	pool, err := db.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte("1234"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	for _, a := range agents {
		if _, err := pool.Exec(ctx, `
			INSERT INTO agents (id, phone, name, pin_hash) VALUES ($1, $2, $3, $4)
			ON CONFLICT (phone) DO NOTHING`, a.id, a.phone, a.name, string(hash)); err != nil {
			return err
		}
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM visits`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		fmt.Println("seed: visits already present, agents ensured")
		return nil
	}
	for i, shop := range shops {
		a := agents[i%len(agents)]
		captured := time.Now().Add(-time.Duration(i*7) * time.Hour)
		lat, lng := 7.2508+float64(i)*0.0013, 5.2103+float64(i)*0.0011
		if _, err := pool.Exec(ctx, `
			INSERT INTO visits (id, agent_id, shop_name, owner_name, phone, lat, lng, captured_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
			ids.New(ids.Visit), a.id, shop, ownerFor(i), fmt.Sprintf("0813%07d", 1000+i), lat, lng, captured); err != nil {
			return err
		}
	}
	fmt.Printf("seed: %d agents, %d visits\n", len(agents), len(shops))
	return nil
}

func ownerFor(i int) string {
	owners := []string{"Adebayo Ojo", "Ngozi Eze", "Yusuf Aliyu", "Funke Adeyemi", "Ifeoma Nwosu", "Tunde Bakare", "Amaka Udo"}
	return owners[i%len(owners)]
}
