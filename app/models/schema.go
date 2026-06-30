package models

import (
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
)

// PostgreSQL schema for queues (parent queues with deployment config)
const postgresQueuesSchemaSQL = `
CREATE TABLE IF NOT EXISTS queues (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    provider TEXT DEFAULT '',
    deployment JSONB,
    input_schema JSONB,
    output_schema JSONB,
    description TEXT DEFAULT '',
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_queues_name ON queues(name);

CREATE TABLE IF NOT EXISTS sub_queues (
    id SERIAL PRIMARY KEY,
    queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 1,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(queue_id, name)
);

CREATE INDEX IF NOT EXISTS idx_sub_queues_queue_id ON sub_queues(queue_id);
CREATE INDEX IF NOT EXISTS idx_sub_queues_enabled ON sub_queues(enabled);
`

// PostgreSQL schema for vaults
const postgresVaultsSchemaSQL = `
CREATE TABLE IF NOT EXISTS vaults (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    description TEXT DEFAULT '',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_vaults_name ON vaults(name);

CREATE TABLE IF NOT EXISTS vault_entries (
    id SERIAL PRIMARY KEY,
    vault_id INTEGER NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value BYTEA NOT NULL,
    is_secret BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(vault_id, key)
);

CREATE INDEX IF NOT EXISTS idx_vault_entries_vault_id ON vault_entries(vault_id);
CREATE INDEX IF NOT EXISTS idx_vault_entries_key ON vault_entries(vault_id, key);
`

// SQLite schema for queues (parent queues with deployment config)
const sqliteQueuesSchemaSQL = `
CREATE TABLE IF NOT EXISTS queues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    provider TEXT DEFAULT '',
    deployment TEXT,
    input_schema TEXT,
    output_schema TEXT,
    description TEXT DEFAULT '',
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_queues_name ON queues(name);

CREATE TABLE IF NOT EXISTS sub_queues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    queue_id INTEGER NOT NULL REFERENCES queues(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 1,
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(queue_id, name)
);

CREATE INDEX IF NOT EXISTS idx_sub_queues_queue_id ON sub_queues(queue_id);
CREATE INDEX IF NOT EXISTS idx_sub_queues_enabled ON sub_queues(enabled);
`

// SQLite schema for vaults
const sqliteVaultsSchemaSQL = `
CREATE TABLE IF NOT EXISTS vaults (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    description TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_vaults_name ON vaults(name);

CREATE TABLE IF NOT EXISTS vault_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vault_id INTEGER NOT NULL REFERENCES vaults(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value BLOB NOT NULL,
    is_secret INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(vault_id, key)
);

CREATE INDEX IF NOT EXISTS idx_vault_entries_vault_id ON vault_entries(vault_id);
CREATE INDEX IF NOT EXISTS idx_vault_entries_key ON vault_entries(vault_id, key);
`

// PostgreSQL schema for GPU autoscaling (provider configs + tracked instances)
const postgresAutoscaleSchemaSQL = `
CREATE TABLE IF NOT EXISTS autoscale_providers (
    id SERIAL PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    provider_type TEXT NOT NULL,
    config BYTEA NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_autoscale_providers_name ON autoscale_providers(name);

CREATE TABLE IF NOT EXISTS autoscale_instances (
    id SERIAL PRIMARY KEY,
    instance_id TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL,
    queue TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'provisioning',
    worker_id TEXT DEFAULT '',
    protected BOOLEAN DEFAULT FALSE,
    price_per_hour DOUBLE PRECISION DEFAULT 0,
    cost_accumulated DOUBLE PRECISION DEFAULT 0,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_job_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_autoscale_instances_queue ON autoscale_instances(queue);
CREATE INDEX IF NOT EXISTS idx_autoscale_instances_status ON autoscale_instances(status);
`

// SQLite schema for GPU autoscaling (provider configs + tracked instances)
const sqliteAutoscaleSchemaSQL = `
CREATE TABLE IF NOT EXISTS autoscale_providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    provider_type TEXT NOT NULL,
    config BLOB NOT NULL,
    enabled INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_autoscale_providers_name ON autoscale_providers(name);

CREATE TABLE IF NOT EXISTS autoscale_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id TEXT UNIQUE NOT NULL,
    provider TEXT NOT NULL,
    queue TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'provisioning',
    worker_id TEXT DEFAULT '',
    protected INTEGER DEFAULT 0,
    price_per_hour REAL DEFAULT 0,
    cost_accumulated REAL DEFAULT 0,
    metadata TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_job_at DATETIME,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_autoscale_instances_queue ON autoscale_instances(queue);
CREATE INDEX IF NOT EXISTS idx_autoscale_instances_status ON autoscale_instances(status);
`

// PostgreSQL schema for admin_user (authentication)
const postgresAdminUserSchemaSQL = `
CREATE TABLE IF NOT EXISTS admin_user (
    id SERIAL PRIMARY KEY,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_admin_user_email ON admin_user(email);
`

// SQLite schema for admin_user (authentication)
const sqliteAdminUserSchemaSQL = `
CREATE TABLE IF NOT EXISTS admin_user (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_admin_user_email ON admin_user(email);
`

// EnsureSchema checks if required tables exist and creates them if they don't.
// This is called at application startup to ensure the database is ready.
// The debug parameter controls whether verbose schema logs are printed.
func EnsureSchema(db *sqlx.DB, debug bool) error {
	// Check for old queue_workers_config table and migrate if needed
	oldTableExists, err := tableExists(db, "queue_workers_config")
	if err != nil {
		return fmt.Errorf("failed to check old table existence: %w", err)
	}

	if oldTableExists {
		if debug {
			log.Println("[SCHEMA] Found old 'queue_workers_config' table, dropping it...")
		}
		if _, err := db.Exec("DROP TABLE IF EXISTS queue_workers_config"); err != nil {
			return fmt.Errorf("failed to drop old table: %w", err)
		}
		if debug {
			log.Println("[SCHEMA] Old 'queue_workers_config' table dropped. Run 'runqy config reload' to re-populate from YAML files.")
		}
	}

	// Check and create queues/sub_queues tables
	queuesExist, err := tableExists(db, "queues")
	if err != nil {
		return fmt.Errorf("failed to check queues table existence: %w", err)
	}

	if queuesExist {
		if debug {
			log.Println("[SCHEMA] Tables 'queues' and 'sub_queues' already exist")
		}
		// Run migrations for existing tables
		if err := migrateSubQueuesEnabled(db, debug); err != nil {
			return fmt.Errorf("failed to migrate sub_queues enabled column: %w", err)
		}
	} else {
		if debug {
			log.Println("[SCHEMA] Creating tables 'queues' and 'sub_queues'...")
		}
		if err := createQueuesSchema(db); err != nil {
			return fmt.Errorf("failed to create queues schema: %w", err)
		}
		if debug {
			log.Println("[SCHEMA] Tables 'queues' and 'sub_queues' created successfully")
		}
	}

	// Check and create vault tables
	vaultsExist, err := tableExists(db, "vaults")
	if err != nil {
		return fmt.Errorf("failed to check vaults table existence: %w", err)
	}

	if vaultsExist {
		if debug {
			log.Println("[SCHEMA] Vaults tables already exist")
		}
	} else {
		if debug {
			log.Println("[SCHEMA] Creating vaults tables...")
		}
		if err := createVaultsSchema(db); err != nil {
			return fmt.Errorf("failed to create vaults schema: %w", err)
		}
		if debug {
			log.Println("[SCHEMA] Vaults tables created successfully")
		}
	}

	// Check and create autoscale tables
	autoscaleExist, err := tableExists(db, "autoscale_instances")
	if err != nil {
		return fmt.Errorf("failed to check autoscale tables existence: %w", err)
	}
	if autoscaleExist {
		if debug {
			log.Println("[SCHEMA] Autoscale tables already exist")
		}
	} else {
		if debug {
			log.Println("[SCHEMA] Creating autoscale tables...")
		}
		if err := createAutoscaleSchema(db); err != nil {
			return fmt.Errorf("failed to create autoscale schema: %w", err)
		}
		if debug {
			log.Println("[SCHEMA] Autoscale tables created successfully")
		}
	}

	// Check and create admin_user table
	adminUserExists, err := tableExists(db, "admin_user")
	if err != nil {
		return fmt.Errorf("failed to check admin_user table existence: %w", err)
	}

	if adminUserExists {
		if debug {
			log.Println("[SCHEMA] Admin user table already exists")
		}
	} else {
		if debug {
			log.Println("[SCHEMA] Creating admin_user table...")
		}
		if err := createAdminUserSchema(db); err != nil {
			return fmt.Errorf("failed to create admin_user schema: %w", err)
		}
		if debug {
			log.Println("[SCHEMA] Admin user table created successfully")
		}
	}

	return nil
}

// tableExists checks if a table exists (works for both PostgreSQL and SQLite)
func tableExists(db *sqlx.DB, tableName string) (bool, error) {
	driverName := db.DriverName()

	var exists bool
	var query string

	if driverName == "sqlite" {
		// SQLite uses sqlite_master
		query = `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`
	} else {
		// PostgreSQL uses information_schema
		query = `
			SELECT EXISTS (
				SELECT FROM information_schema.tables
				WHERE table_schema = 'public'
				AND table_name = $1
			)
		`
	}

	err := db.Get(&exists, query, tableName)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// createQueuesSchema creates the queues and sub_queues tables based on the database driver
func createQueuesSchema(db *sqlx.DB) error {
	driverName := db.DriverName()

	var schemaSQL string
	if driverName == "sqlite" {
		schemaSQL = sqliteQueuesSchemaSQL
	} else {
		schemaSQL = postgresQueuesSchemaSQL
	}

	_, err := db.Exec(schemaSQL)
	return err
}

// createVaultsSchema creates the vaults tables based on the database driver
func createVaultsSchema(db *sqlx.DB) error {
	driverName := db.DriverName()

	var schemaSQL string
	if driverName == "sqlite" {
		schemaSQL = sqliteVaultsSchemaSQL
	} else {
		schemaSQL = postgresVaultsSchemaSQL
	}

	_, err := db.Exec(schemaSQL)
	return err
}

// createAutoscaleSchema creates the autoscale tables based on the database driver
func createAutoscaleSchema(db *sqlx.DB) error {
	driverName := db.DriverName()

	var schemaSQL string
	if driverName == "sqlite" {
		schemaSQL = sqliteAutoscaleSchemaSQL
	} else {
		schemaSQL = postgresAutoscaleSchemaSQL
	}

	_, err := db.Exec(schemaSQL)
	return err
}

// createAdminUserSchema creates the admin_user table based on the database driver
func createAdminUserSchema(db *sqlx.DB) error {
	driverName := db.DriverName()

	var schemaSQL string
	if driverName == "sqlite" {
		schemaSQL = sqliteAdminUserSchemaSQL
	} else {
		schemaSQL = postgresAdminUserSchemaSQL
	}

	_, err := db.Exec(schemaSQL)
	return err
}

// migrateSubQueuesEnabled adds the 'enabled' column to sub_queues if it doesn't exist
func migrateSubQueuesEnabled(db *sqlx.DB, debug bool) error {
	driverName := db.DriverName()

	// Check if column exists
	var hasColumn bool
	if driverName == "sqlite" {
		// SQLite: check pragma table_info
		var count int
		err := db.Get(&count, `SELECT COUNT(*) FROM pragma_table_info('sub_queues') WHERE name = 'enabled'`)
		if err != nil {
			return fmt.Errorf("failed to check for enabled column: %w", err)
		}
		hasColumn = count > 0
	} else {
		// PostgreSQL: check information_schema
		err := db.Get(&hasColumn, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'sub_queues' AND column_name = 'enabled'
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to check for enabled column: %w", err)
		}
	}

	if hasColumn {
		return nil // Column already exists
	}

	if debug {
		log.Println("[SCHEMA] Adding 'enabled' column to sub_queues table...")
	}

	if driverName == "sqlite" {
		// SQLite migration
		if _, err := db.Exec(`ALTER TABLE sub_queues ADD COLUMN enabled INTEGER DEFAULT 1`); err != nil {
			return fmt.Errorf("failed to add enabled column: %w", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sub_queues_enabled ON sub_queues(enabled)`); err != nil {
			return fmt.Errorf("failed to create enabled index: %w", err)
		}
	} else {
		// PostgreSQL migration
		if _, err := db.Exec(`ALTER TABLE sub_queues ADD COLUMN enabled BOOLEAN DEFAULT TRUE`); err != nil {
			return fmt.Errorf("failed to add enabled column: %w", err)
		}
		if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_sub_queues_enabled ON sub_queues(enabled)`); err != nil {
			return fmt.Errorf("failed to create enabled index: %w", err)
		}
	}

	if debug {
		log.Println("[SCHEMA] Added 'enabled' column to sub_queues table")
	}
	return nil
}
