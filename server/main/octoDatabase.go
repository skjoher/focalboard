package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func connectDatabase(dbType string, connectionString string) {
	log.Println("connectDatabase")
	var err error

	db, err = sql.Open(dbType, connectionString)
	if err != nil {
		log.Fatal("connectDatabase: ", err)
		panic(err)
	}

	err = db.Ping()
	if err != nil {
		log.Println(`Database Ping failed`)
		panic(err)
	}

	createTablesIfNotExists(dbType)
}

// Block is the basic data unit
type Block struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Type     string `json:"type"`
	CreateAt int64  `json:"createAt"`
	UpdateAt int64  `json:"updateAt"`
	DeleteAt int64  `json:"deleteAt"`
}

func createTablesIfNotExists(dbType string) {
	// TODO: Add update_by with the user's ID
	// TODO: Consolidate insert_at and update_at, decide if the server of DB should set it
	var query string
	if dbType == "sqlite3" {
		query = `CREATE TABLE IF NOT EXISTS blocks (
			id VARCHAR(36),
			insert_at DATETIME NOT NULL DEFAULT current_timestamp,
			parent_id VARCHAR(36),
			type TEXT,
			json TEXT,
			create_at BIGINT,
			update_at BIGINT,
			delete_at BIGINT,
			PRIMARY KEY (id, insert_at)
		);`
	} else {
		query = `CREATE TABLE IF NOT EXISTS blocks (
			id VARCHAR(36),
			insert_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			parent_id VARCHAR(36),
			type TEXT,
			json TEXT,
			create_at BIGINT,
			update_at BIGINT,
			delete_at BIGINT,
			PRIMARY KEY (id, insert_at)
		);`
	}

	_, err := db.Exec(query)
	if err != nil {
		log.Fatal("createTablesIfNotExists: ", err)
		panic(err)
	}
	log.Printf("createTablesIfNotExists(%s)", dbType)
}

func blockFromMap(m map[string]interface{}) Block {
	var b Block
	b.ID = m["id"].(string)
	// Parent ID can be nil (for now)
	if m["parentId"] != nil {
		b.ParentID = m["parentId"].(string)
	}
	// Allow nil type for imports
	if m["type"] != nil {
		b.Type = m["type"].(string)
	}
	if m["createAt"] != nil {
		b.CreateAt = int64(m["createAt"].(float64))
	}
	if m["updateAt"] != nil {
		b.UpdateAt = int64(m["updateAt"].(float64))
	}
	if m["deleteAt"] != nil {
		b.DeleteAt = int64(m["deleteAt"].(float64))
	}

	return b
}

func getBlocksWithParentAndType(parentID string, blockType string) []string {
	query := `WITH latest AS
		(
			SELECT * FROM
			(
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY id ORDER BY insert_at DESC) AS rn
				FROM blocks
			) a
			WHERE rn = 1
		)

		SELECT COALESCE("json", '{}')
		FROM latest
		WHERE delete_at = 0 and parent_id = $1 and type = $2`

	rows, err := db.Query(query, parentID, blockType)
	if err != nil {
		log.Printf(`getBlocksWithParentAndType ERROR: %v`, err)
		panic(err)
	}

	return blocksFromRows(rows)
}

func getBlocksWithParent(parentID string) []string {
	query := `WITH latest AS
		(
			SELECT * FROM
			(
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY id ORDER BY insert_at DESC) AS rn
				FROM blocks
			) a
			WHERE rn = 1
		)

		SELECT COALESCE("json", '{}')
		FROM latest
		WHERE delete_at = 0 and parent_id = $1`

	rows, err := db.Query(query, parentID)
	if err != nil {
		log.Printf(`getBlocksWithParent ERROR: %v`, err)
		panic(err)
	}

	return blocksFromRows(rows)
}

func getSubTree(blockID string) []string {
	query := `WITH latest AS
	(
		SELECT * FROM
		(
			SELECT
				*,
				ROW_NUMBER() OVER (PARTITION BY id ORDER BY insert_at DESC) AS rn
			FROM blocks
		) a
		WHERE rn = 1
	)

	SELECT COALESCE("json", '{}')
	FROM latest
	WHERE delete_at = 0
		AND (id = $1
			OR parent_id = $1)`

	rows, err := db.Query(query, blockID)
	if err != nil {
		log.Printf(`getSubTree ERROR: %v`, err)
		panic(err)
	}

	return blocksFromRows(rows)
}

func getAllBlocks() []string {
	query := `WITH latest AS
	(
		SELECT * FROM
		(
			SELECT
				*,
				ROW_NUMBER() OVER (PARTITION BY id ORDER BY insert_at DESC) AS rn
			FROM blocks
		) a
		WHERE rn = 1
	)

	SELECT COALESCE("json", '{}')
	FROM latest
	WHERE delete_at = 0`

	rows, err := db.Query(query)
	if err != nil {
		log.Printf(`getAllBlocks ERROR: %v`, err)
		panic(err)
	}

	return blocksFromRows(rows)
}

func blocksFromRows(rows *sql.Rows) []string {
	defer rows.Close()

	var results []string

	for rows.Next() {
		var json string
		err := rows.Scan(&json)
		if err != nil {
			// handle this error
			log.Printf(`blocksFromRows ERROR: %v`, err)
			panic(err)
		}

		results = append(results, json)
	}

	return results
}

func getParentID(blockID string) string {
	statement :=
		`WITH latest AS
		(
			SELECT * FROM
			(
				SELECT
					*,
					ROW_NUMBER() OVER (PARTITION BY id ORDER BY insert_at DESC) AS rn
				FROM blocks
			) a
			WHERE rn = 1
		)

		SELECT parent_id
		FROM latest
		WHERE delete_at = 0
			AND id = $1`

	row := db.QueryRow(statement, blockID)

	var parentID string
	err := row.Scan(&parentID)
	if err != nil {
		return ""
	}

	return parentID
}

func insertBlock(block Block, json string) {
	statement := `INSERT INTO blocks(id, parent_id, type, json, create_at, update_at, delete_at) VALUES($1, $2, $3, $4, $5, $6, $7)`
	_, err := db.Exec(statement, block.ID, block.ParentID, block.Type, json, block.CreateAt, block.UpdateAt, block.DeleteAt)
	if err != nil {
		panic(err)
	}
}

func deleteBlock(blockID string) {
	now := time.Now().Unix()
	json := fmt.Sprintf(`{"id":"%s","updateAt":%d,"deleteAt":%d}`, blockID, now, now)
	statement := `INSERT INTO blocks(id, json, update_at, delete_at) VALUES($1, $2, $3, $4)`
	_, err := db.Exec(statement, blockID, json, now, now)
	if err != nil {
		panic(err)
	}
}
