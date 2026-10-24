package cli

import (
	"fmt"
	"strings"
)

// preset is a shortcut for a service people add over and over. It is a
// convenience, never a limit: "another image" covers everything else, and the
// generated spec is plain YAML to edit afterwards.
type preset struct {
	Label   string
	Service string
	Image   string
	Vars    map[string]string // ${NAME} references, so values stay in .env
	Volume  string            // "name:/path", empty for a stateless service
	Health  []string
	Memory  string

	// HostVar is the env key an application usually reads to find this
	// service. The value is the service name, which is its network alias.
	HostVar string
}

var presets = []preset{
	{
		Label: "PostgreSQL", Service: "db", Image: "postgres:17-alpine",
		Vars: map[string]string{
			"POSTGRES_DB":       "${DB_DATABASE}",
			"POSTGRES_USER":     "${DB_USERNAME}",
			"POSTGRES_PASSWORD": "${DB_PASSWORD}",
		},
		Volume: "dbdata:/var/lib/postgresql/data",
		Health: []string{"pg_isready"},
		Memory: "256m", HostVar: "DB_HOST",
	},
	{
		Label: "MySQL", Service: "db", Image: "mysql:8.4",
		Vars: map[string]string{
			"MYSQL_DATABASE":      "${DB_DATABASE}",
			"MYSQL_USER":          "${DB_USERNAME}",
			"MYSQL_PASSWORD":      "${DB_PASSWORD}",
			"MYSQL_ROOT_PASSWORD": "${DB_ROOT_PASSWORD}",
		},
		Volume: "dbdata:/var/lib/mysql",
		Health: []string{"mysqladmin", "ping", "-h", "127.0.0.1"},
		Memory: "512m", HostVar: "DB_HOST",
	},
	{
		Label: "MariaDB", Service: "db", Image: "mariadb:11.4",
		Vars: map[string]string{
			"MARIADB_DATABASE":      "${DB_DATABASE}",
			"MARIADB_USER":          "${DB_USERNAME}",
			"MARIADB_PASSWORD":      "${DB_PASSWORD}",
			"MARIADB_ROOT_PASSWORD": "${DB_ROOT_PASSWORD}",
		},
		Volume: "dbdata:/var/lib/mysql",
		Health: []string{"healthcheck.sh", "--connect"},
		Memory: "512m", HostVar: "DB_HOST",
	},
	{
		Label: "Redis", Service: "redis", Image: "redis:7-alpine",
		Volume: "redisdata:/data",
		Health: []string{"redis-cli", "ping"},
		Memory: "128m", HostVar: "REDIS_HOST",
	},
	{
		Label: "MongoDB", Service: "mongo", Image: "mongo:8",
		Vars: map[string]string{
			"MONGO_INITDB_ROOT_USERNAME": "${DB_USERNAME}",
			"MONGO_INITDB_ROOT_PASSWORD": "${DB_PASSWORD}",
		},
		Volume: "mongodata:/data/db",
		Health: []string{"mongosh", "--quiet", "--eval", "db.adminCommand('ping')"},
		Memory: "512m", HostVar: "DB_HOST",
	},
}

// Labels stay unstyled: the selector colours the whole line, and a nested
// style would reset the colour halfway through.
func presetLabels() []string {
	out := make([]string, 0, len(presets)+1)
	for _, p := range presets {
		out = append(out, fmt.Sprintf("%s  %s", p.Label, p.Image))
	}
	return append(out, "another image")
}

// yamlList renders a flow-style list, quoting every element so a healthcheck
// argument never has to be guessed at.
func yamlList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
